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
	"time"

	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/policy"
	"github.com/famomatic/ytv1/internal/types"
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
	ctx, cancel := withRequestTimeout(ctx, e.config.RequestTimeout)
	defer cancel()

	clients := e.selector.Select(videoID)
	if !e.config.DisableFallbackClients {
		clients = e.withFallbackClients(clients)
	}
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
	requiredProtocols := make([]innertube.VideoStreamingProtocol, 0, 3)
	recommendedExists := false
	for _, protocol := range []innertube.VideoStreamingProtocol{
		innertube.StreamingProtocolHTTPS,
		innertube.StreamingProtocolDASH,
		innertube.StreamingProtocolHLS,
	} {
		p := effectivePoTokenFetchPolicy(profile, protocol, e.config.PoTokenFetchPolicy)
		switch p {
		case innertube.PoTokenFetchPolicyRequired:
			requiredProtocols = append(requiredProtocols, protocol)
		case innertube.PoTokenFetchPolicyRecommended:
			recommendedExists = true
		}
	}

	if len(requiredProtocols) == 0 && !recommendedExists {
		return nil
	}

	if e.config.PoTokenProvider == nil {
		if len(requiredProtocols) > 0 {
			return &PoTokenRequiredError{
				Client: profile.Name,
				Cause:  "provider missing (required by policy)",
			}
		}
		return nil
	}

	token, err := e.config.PoTokenProvider.GetToken(ctx, profile.Name)
	if err != nil {
		if len(requiredProtocols) > 0 {
			return &PoTokenRequiredError{
				Client: profile.Name,
				Cause:  "provider error: " + err.Error(),
			}
		}
		return nil
	}
	if token == "" {
		if len(requiredProtocols) > 0 {
			return &PoTokenRequiredError{
				Client: profile.Name,
				Cause:  "empty token from provider",
			}
		}
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

func effectivePoTokenFetchPolicy(
	profile innertube.ClientProfile,
	protocol innertube.VideoStreamingProtocol,
	overrides map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy,
) innertube.PoTokenFetchPolicy {
	if overrides != nil {
		if override, ok := overrides[protocol]; ok {
			return normalizePoTokenFetchPolicy(override)
		}
	}

	// Keep request stage non-blocking by default for compatibility.
	// Strict behavior can be enabled via overrides.
	if requiresPoToken(profile, protocol) || recommendsPoToken(profile, protocol) {
		return innertube.PoTokenFetchPolicyRecommended
	}
	return innertube.PoTokenFetchPolicyNever
}

func normalizePoTokenFetchPolicy(p innertube.PoTokenFetchPolicy) innertube.PoTokenFetchPolicy {
	switch strings.ToLower(strings.TrimSpace(string(p))) {
	case string(innertube.PoTokenFetchPolicyRequired):
		return innertube.PoTokenFetchPolicyRequired
	case string(innertube.PoTokenFetchPolicyRecommended):
		return innertube.PoTokenFetchPolicyRecommended
	case string(innertube.PoTokenFetchPolicyNever):
		return innertube.PoTokenFetchPolicyNever
	default:
		return innertube.PoTokenFetchPolicyRecommended
	}
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

	// Add global request headers last so caller can override defaults.
	for k, values := range e.config.RequestHeaders {
		for _, val := range values {
			httpReq.Header.Add(k, val)
		}
	}

	metaCfg := normalizeMetadataTransportConfig(e.config.MetadataTransport)
	var lastErr error
	for attempt := 0; attempt <= metaCfg.MaxRetries; attempt++ {
		playerResp, err := e.fetchOnce(ctx, httpReq, profile)
		if err == nil {
			return playerResp, nil
		}
		lastErr = err
		if !isRetryableMetadataError(err, metaCfg) || attempt == metaCfg.MaxRetries {
			return nil, err
		}
		if err := waitMetadataBackoff(ctx, metaCfg.backoffFor(attempt)); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
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

func (e *Engine) fetchOnce(ctx context.Context, template *http.Request, profile innertube.ClientProfile) (*innertube.PlayerResponse, error) {
	httpReq := template.Clone(ctx)
	httpReq.Body, _ = template.GetBody()

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var playerResp innertube.PlayerResponse
	if err := json.Unmarshal(respBody, &playerResp); err != nil {
		return nil, err
	}

	if !playerResp.PlayabilityStatus.IsOK() && !playerResp.PlayabilityStatus.IsLive() {
		return nil, &PlayabilityError{
			Client: profile.Name,
			Status: playerResp.PlayabilityStatus.Status,
			Reason: playerResp.PlayabilityStatus.Reason,
		}
	}
	return &playerResp, nil
}

type effectiveMetadataTransportConfig struct {
	MaxRetries       int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	RetryStatusCodes []int
}

func normalizeMetadataTransportConfig(cfg innertube.MetadataTransportConfig) effectiveMetadataTransportConfig {
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	initialBackoff := cfg.InitialBackoff
	if initialBackoff <= 0 {
		initialBackoff = 250 * time.Millisecond
	}
	maxBackoff := cfg.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 2 * time.Second
	}
	statusCodes := cfg.RetryStatusCodes
	if len(statusCodes) == 0 {
		statusCodes = []int{
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}
	}
	return effectiveMetadataTransportConfig{
		MaxRetries:       maxRetries,
		InitialBackoff:   initialBackoff,
		MaxBackoff:       maxBackoff,
		RetryStatusCodes: statusCodes,
	}
}

func (c effectiveMetadataTransportConfig) backoffFor(attempt int) time.Duration {
	backoff := c.InitialBackoff
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff > c.MaxBackoff {
			return c.MaxBackoff
		}
	}
	return backoff
}

func isRetryableMetadataError(err error, cfg effectiveMetadataTransportConfig) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		for _, code := range cfg.RetryStatusCodes {
			if httpErr.StatusCode == code {
				return true
			}
		}
		return false
	}
	var playErr *PlayabilityError
	if errors.As(err, &playErr) {
		return false
	}
	return true
}

func waitMetadataBackoff(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withRequestTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}
