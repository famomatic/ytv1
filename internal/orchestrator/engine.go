package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"

	"github.com/mjmst/ytv1/internal/innertube"
	"github.com/mjmst/ytv1/internal/policy"
	"github.com/mjmst/ytv1/internal/types"
)

// Engine is the main orchestrator for video extraction.
type Engine struct {
	selector       policy.Selector
	config         innertube.Config
	apiKeyResolver *innertube.APIKeyResolver
}

func NewEngine(selector policy.Selector, config innertube.Config) *Engine {
	engine := &Engine{
		selector: selector,
		config:   config,
	}
	if config.EnableDynamicAPIKeyResolution {
		engine.apiKeyResolver = innertube.NewAPIKeyResolver(config.HTTPClient)
	}
	return engine
}

type extractionResult struct {
	response *innertube.PlayerResponse
	err      error
	client   string
}

// GetVideoInfo fetches video info using the configured policy and clients.
// It implements the "Racing" extraction proposal.
func (e *Engine) GetVideoInfo(ctx context.Context, videoID string) (*innertube.PlayerResponse, error) {
	clients := e.selector.Select(videoID)
	clients = e.withFallbackClients(clients)
	if len(clients) == 0 {
		return nil, types.ErrNoClientsAvailable
	}

	primary, fallback := splitClientPhases(clients)

	resp, attempts := e.tryPhase(ctx, videoID, primary)
	if resp != nil {
		return resp, nil
	}

	if len(fallback) > 0 && shouldRunFallbackPhase(attempts) {
		fallbackResp, fallbackAttempts := e.tryPhase(ctx, videoID, fallback)
		if fallbackResp != nil {
			return fallbackResp, nil
		}
		attempts = append(attempts, fallbackAttempts...)
	}

	if len(attempts) > 0 {
		return nil, &AllClientsFailedError{Attempts: attempts}
	}
	return nil, types.ErrNoClientsAvailable
}

func (e *Engine) tryPhase(ctx context.Context, videoID string, clients []innertube.ClientProfile) (*innertube.PlayerResponse, []AttemptError) {
	if len(clients) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan extractionResult, len(clients))
	var wg sync.WaitGroup

	for _, profile := range clients {
		wg.Add(1)
		go func(p innertube.ClientProfile) {
			defer wg.Done()

			if ctx.Err() != nil {
				return
			}

			req := innertube.NewPlayerRequest(p, videoID, innertube.PlayerRequestOptions{
				VisitorData: e.config.VisitorData,
			})
			if err := e.applyPoToken(ctx, req, p); err != nil {
				select {
				case results <- extractionResult{response: nil, err: err, client: p.Name}:
				case <-ctx.Done():
				}
				return
			}
			resp, err := e.fetch(ctx, req, p)

			select {
			case results <- extractionResult{response: resp, err: err, client: p.Name}:
			case <-ctx.Done():
			}
		}(profile)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var attempts []AttemptError
	for res := range results {
		if res.err == nil {
			cancel()
			return res.response, attempts
		}
		attempts = append(attempts, AttemptError{
			Client: res.client,
			Err:    res.err,
		})
	}
	return nil, attempts
}

func (e *Engine) withFallbackClients(clients []innertube.ClientProfile) []innertube.ClientProfile {
	if len(clients) == 0 {
		return clients
	}
	registry := e.selector.Registry()
	if registry == nil {
		return clients
	}
	out := append([]innertube.ClientProfile(nil), clients...)
	seenFallback := map[string]struct{}{}
	for _, c := range out {
		if isFallbackClient(c) {
			seenFallback[strings.ToUpper(strings.TrimSpace(c.Name))] = struct{}{}
		}
	}
	appendIfMissing := func(alias string) {
		p, ok := registry.Get(alias)
		if !ok {
			return
		}
		name := strings.ToUpper(strings.TrimSpace(p.Name))
		if _, exists := seenFallback[name]; exists {
			return
		}
		out = append(out, p)
		seenFallback[name] = struct{}{}
	}
	appendIfMissing("web_embedded")
	appendIfMissing("tv")
	return out
}

func (e *Engine) applyPoToken(ctx context.Context, req *innertube.PlayerRequest, profile innertube.ClientProfile) error {
	policy, hasPolicy := profile.PoTokenPolicy[innertube.StreamingProtocolHTTPS]
	if !hasPolicy || (!policy.Required && !policy.Recommended) {
		return nil
	}
	if e.config.PoTokenProvider == nil {
		// Non-blocking: proceed without token when provider is not configured.
		return nil
	}
	token, err := e.config.PoTokenProvider.GetToken(ctx, profile.Name)
	if err != nil {
		// Non-blocking: proceed without token when provider fails.
		return nil
	}
	if token == "" {
		// Non-blocking: proceed without token when provider returns empty token.
		return nil
	}
	req.SetPoToken(token)
	return nil
}

func requiresPoToken(profile innertube.ClientProfile, protocol innertube.VideoStreamingProtocol) bool {
	policy, ok := profile.PoTokenPolicy[protocol]
	if !ok {
		return false
	}
	return policy.Required
}

func recommendsPoToken(profile innertube.ClientProfile, protocol innertube.VideoStreamingProtocol) bool {
	policy, ok := profile.PoTokenPolicy[protocol]
	if !ok {
		return false
	}
	return policy.Recommended
}

func splitClientPhases(clients []innertube.ClientProfile) ([]innertube.ClientProfile, []innertube.ClientProfile) {
	var primary []innertube.ClientProfile
	var fallback []innertube.ClientProfile
	for _, c := range clients {
		if isFallbackClient(c) {
			fallback = append(fallback, c)
			continue
		}
		primary = append(primary, c)
	}
	return primary, fallback
}

func isFallbackClient(c innertube.ClientProfile) bool {
	name := strings.ToUpper(strings.TrimSpace(c.Name))
	return name == "WEB_EMBEDDED_PLAYER" || name == "TVHTML5"
}

func shouldRunFallbackPhase(attempts []AttemptError) bool {
	for _, attempt := range attempts {
		var pErr *PlayabilityError
		if !errors.As(attempt.Err, &pErr) {
			var poErr *PoTokenRequiredError
			if errors.As(attempt.Err, &poErr) {
				return true
			}
			continue
		}
		// Keep fallback targeted to known playability gating classes.
		if pErr.RequiresLogin() || pErr.IsAgeRestricted() || pErr.IsGeoRestricted() || pErr.IsUnavailable() {
			return true
		}
	}
	return false
}

func (e *Engine) fetch(ctx context.Context, req *innertube.PlayerRequest, profile innertube.ClientProfile) (*innertube.PlayerResponse, error) {
	// Construct URL
	apiKey := e.resolveAPIKey(ctx, profile)
	url := "https://" + profile.Host + "/youtubei/v1/player"
	if apiKey != "" {
		url += "?key=" + neturl.QueryEscape(apiKey)
	}

	// Marshaling request
	body, err := innertube.MarshalRequest(req)
	if err != nil {
		return nil, err
	}

	// Create Request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", profile.UserAgent)
	httpReq.Header.Set("Origin", "https://"+profile.Host)
	httpReq.Header.Set("Referer", "https://"+profile.Host+"/watch?v="+req.VideoID)
	// Add other headers from profile
	for k, v := range profile.Headers {
		for _, val := range v {
			httpReq.Header.Add(k, val)
		}
	}

	// Execute
	resp, err := e.config.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{
			Client:     profile.Name,
			StatusCode: resp.StatusCode,
		}
	}

	// Read body for potential debugging
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Parse Response
	var playerResp innertube.PlayerResponse
	if err := json.Unmarshal(respBody, &playerResp); err != nil {
		return nil, err
	}

	// Playability Check
	if !playerResp.PlayabilityStatus.IsOK() && !playerResp.PlayabilityStatus.IsLive() {
		return nil, &PlayabilityError{
			Client: profile.Name,
			Status: playerResp.PlayabilityStatus.Status,
			Reason: playerResp.PlayabilityStatus.Reason,
		}
	}

	return &playerResp, nil
}

func (e *Engine) resolveAPIKey(ctx context.Context, profile innertube.ClientProfile) string {
	if e.apiKeyResolver == nil {
		return profile.APIKey
	}
	key, err := e.apiKeyResolver.Resolve(ctx, profile)
	if err != nil {
		return profile.APIKey
	}
	return key
}
