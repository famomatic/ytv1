package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
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
	order    int
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

	for idx, profile := range clients {
		wg.Add(1)
		go func(order int, p innertube.ClientProfile) {
			defer wg.Done()
			clientLabel := profileIDOrName(p)

			if order > 0 && e.config.ClientHedgeDelay > 0 {
				timer := time.NewTimer(time.Duration(order) * e.config.ClientHedgeDelay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}

			if ctx.Err() != nil {
				return
			}
			e.emitExtractionEvent("player_api_json", "start", clientLabel, "")

			req := innertube.NewPlayerRequest(p, videoID, innertube.PlayerRequestOptions{
				VisitorData:        e.resolveVisitorData(ctx, p, videoID),
				SignatureTimestamp: e.resolveSignatureTimestamp(ctx, p, videoID),
				UseAdPlayback:      e.config.UseAdPlaybackContext && p.SupportsAdPlaybackContext,
				PlayerParams:       strings.TrimSpace(p.PlayerParams),
			})
			if err := e.applyPoToken(ctx, req, p); err != nil {
				select {
				case results <- extractionResult{response: nil, err: err, client: clientLabel, order: order}:
				case <-ctx.Done():
				}
				return
			}
			resp, err := e.fetch(ctx, req, p, videoID)

			select {
			case results <- extractionResult{response: resp, err: err, client: clientLabel, order: order}:
			case <-ctx.Done():
			}
		}(idx, profile)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	type orderedResult struct {
		order  int
		client string
		resp   *innertube.PlayerResponse
		err    error
	}
	pending := make(map[int]orderedResult, len(clients))
	nextOrder := 0
	attempts := make([]AttemptError, 0, len(clients))

	// Deterministic client-order selection:
	// keep parallel requests for latency, but only commit a success when all
	// earlier-order clients have already completed (success/failure).
	for res := range results {
		pending[res.order] = orderedResult{
			order:  res.order,
			client: res.client,
			resp:   res.response,
			err:    res.err,
		}
		for {
			current, ok := pending[nextOrder]
			if !ok {
				break
			}
			delete(pending, nextOrder)
			nextOrder++

			if current.err == nil {
				current.resp.SourceClient = current.client
				e.emitExtractionEvent("player_api_json", "success", current.client, "")
				cancel()
				return current.resp, attempts
			}
			e.emitExtractionEvent("player_api_json", "failure", current.client, current.err.Error())
			attempts = append(attempts, AttemptError{
				Client: current.client,
				Err:    current.err,
			})
		}
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
			seenFallback[fallbackKey(c)] = struct{}{}
		}
	}
	appendIfMissing := func(alias string) {
		p, ok := registry.Get(alias)
		if !ok {
			return
		}
		key := fallbackKey(p)
		if _, exists := seenFallback[key]; exists {
			return
		}
		out = append(out, p)
		seenFallback[key] = struct{}{}
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
				Client:            profile.Name,
				Cause:             "provider missing (required by policy)",
				Policy:            innertube.PoTokenFetchPolicyRequired,
				Protocols:         append([]innertube.VideoStreamingProtocol(nil), requiredProtocols...),
				ProviderAvailable: false,
			}
		}
		return nil
	}

	token, err := e.config.PoTokenProvider.GetToken(ctx, profile.Name)
	if err != nil {
		if len(requiredProtocols) > 0 {
			return &PoTokenRequiredError{
				Client:            profile.Name,
				Cause:             "provider error: " + err.Error(),
				Policy:            innertube.PoTokenFetchPolicyRequired,
				Protocols:         append([]innertube.VideoStreamingProtocol(nil), requiredProtocols...),
				ProviderAvailable: true,
			}
		}
		return nil
	}
	if token == "" {
		if len(requiredProtocols) > 0 {
			return &PoTokenRequiredError{
				Client:            profile.Name,
				Cause:             "empty token from provider",
				Policy:            innertube.PoTokenFetchPolicyRequired,
				Protocols:         append([]innertube.VideoStreamingProtocol(nil), requiredProtocols...),
				ProviderAvailable: true,
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
	id := strings.ToLower(strings.TrimSpace(c.ID))
	if id == "web_embedded" || id == "web_embedded_player" || id == "tv" || id == "tvhtml5" || id == "tv_downgraded" {
		return true
	}
	name := strings.ToUpper(strings.TrimSpace(c.Name))
	return name == "WEB_EMBEDDED_PLAYER" || name == "TVHTML5"
}

func fallbackKey(c innertube.ClientProfile) string {
	if id := strings.ToLower(strings.TrimSpace(c.ID)); id != "" {
		if strings.HasPrefix(id, "web_embedded") {
			return "web_embedded"
		}
		if strings.HasPrefix(id, "tv") {
			return "tv"
		}
	}
	name := strings.ToUpper(strings.TrimSpace(c.Name))
	if name == "WEB_EMBEDDED_PLAYER" {
		return "web_embedded"
	}
	if name == "TVHTML5" {
		return "tv"
	}
	return strings.ToLower(strings.TrimSpace(name))
}

func profileIDOrName(c innertube.ClientProfile) string {
	if id := strings.TrimSpace(c.ID); id != "" {
		return id
	}
	return c.Name
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

func (e *Engine) fetch(ctx context.Context, req *innertube.PlayerRequest, profile innertube.ClientProfile, videoID string) (*innertube.PlayerResponse, error) {
	// Construct URL
	apiKey := e.resolveAPIKey(ctx, profile, videoID)
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
	origin := "https://" + profile.Host
	httpReq.Header.Set("Origin", origin)
	httpReq.Header.Set("X-Origin", origin)
	httpReq.Header.Set("Referer", origin+"/watch?v="+req.VideoID)
	if profile.ContextNameID > 0 {
		httpReq.Header.Set("X-YouTube-Client-Name", strconv.Itoa(profile.ContextNameID))
	}
	if profile.Version != "" {
		httpReq.Header.Set("X-YouTube-Client-Version", profile.Version)
	}
	if req.Context.Client.VisitorData != "" {
		httpReq.Header.Set("X-Goog-Visitor-Id", req.Context.Client.VisitorData)
	}
	if profile.SupportsCookies {
		cookieAuth := innertube.BuildCookieAuthHeaders(e.config.HTTPClient, profile.Host, time.Now(), e.resolveCookieAuthContext(ctx, profile, videoID))
		for k, values := range cookieAuth {
			for _, val := range values {
				httpReq.Header.Add(k, val)
			}
		}
	}
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

func (e *Engine) resolveAPIKey(ctx context.Context, profile innertube.ClientProfile, videoID string) string {
	if e.apiKeyResolver == nil {
		return profile.APIKey
	}
	key, err := e.apiKeyResolver.Resolve(ctx, profile, videoID)
	if err != nil {
		return profile.APIKey
	}
	return key
}

func (e *Engine) resolveVisitorData(ctx context.Context, profile innertube.ClientProfile, videoID string) string {
	if configured := strings.TrimSpace(e.config.VisitorData); configured != "" {
		return configured
	}
	if cookie := innertube.ResolveVisitorData(e.config.HTTPClient, profile.Host, ""); cookie != "" {
		return cookie
	}
	if e.apiKeyResolver != nil {
		if fromWatch := strings.TrimSpace(e.apiKeyResolver.ResolveVisitorData(ctx, profile, videoID)); fromWatch != "" {
			return fromWatch
		}
	}
	return ""
}

func (e *Engine) resolveCookieAuthContext(ctx context.Context, profile innertube.ClientProfile, videoID string) innertube.CookieAuthContext {
	if e.apiKeyResolver == nil {
		return innertube.CookieAuthContext{}
	}
	return e.apiKeyResolver.ResolveCookieAuthContext(ctx, profile, videoID)
}

func (e *Engine) resolveSignatureTimestamp(ctx context.Context, profile innertube.ClientProfile, videoID string) int {
	if e.apiKeyResolver == nil {
		return 0
	}
	return e.apiKeyResolver.ResolveSignatureTimestamp(ctx, profile, videoID)
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
		detail := extractPlayabilityDetail(&playerResp)
		return nil, &PlayabilityError{
			Client: profile.Name,
			Status: playerResp.PlayabilityStatus.Status,
			Reason: playerResp.PlayabilityStatus.Reason,
			Detail: detail,
		}
	}
	return &playerResp, nil
}

func extractPlayabilityDetail(resp *innertube.PlayerResponse) PlayabilityDetail {
	if resp == nil {
		return PlayabilityDetail{}
	}
	status := strings.ToUpper(strings.TrimSpace(resp.PlayabilityStatus.Status))
	reason := firstNonEmpty(
		resp.PlayabilityStatus.Reason,
		resp.PlayabilityStatus.Subreason,
		errorScreenReason(resp.PlayabilityStatus.ErrorScreen),
	)
	subreason := firstNonEmpty(
		resp.PlayabilityStatus.Subreason,
		errorScreenSubreason(resp.PlayabilityStatus.ErrorScreen),
	)
	text := strings.ToUpper(strings.TrimSpace(status + " " + reason + " " + subreason))
	countries := append([]string(nil), resp.Microformat.PlayerMicroformatRenderer.AvailableCountries...)
	return PlayabilityDetail{
		Subreason:          subreason,
		AvailableCountries: countries,
		GeoRestricted:      strings.Contains(text, "COUNTRY") || strings.Contains(text, "REGION") || strings.Contains(text, "LOCATION"),
		LoginRequired:      strings.Contains(text, "LOGIN") || strings.Contains(text, "SIGN IN"),
		AgeRestricted:      strings.Contains(text, "AGE"),
		Unavailable:        strings.Contains(text, "UNAVAILABLE") || strings.Contains(text, "PRIVATE") || strings.Contains(text, "DELETED"),
		DRMProtected:       strings.Contains(text, "DRM"),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func errorScreenReason(es *innertube.ErrorScreen) string {
	if es == nil || es.PlayerErrorMessageRenderer == nil {
		return ""
	}
	return langTextToString(es.PlayerErrorMessageRenderer.Reason)
}

func errorScreenSubreason(es *innertube.ErrorScreen) string {
	if es == nil || es.PlayerErrorMessageRenderer == nil {
		return ""
	}
	return langTextToString(es.PlayerErrorMessageRenderer.Subreason)
}

func langTextToString(v innertube.LangText) string {
	if strings.TrimSpace(v.SimpleText) != "" {
		return strings.TrimSpace(v.SimpleText)
	}
	if len(v.Runs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(v.Runs))
	for _, run := range v.Runs {
		if text := strings.TrimSpace(run.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
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

func (e *Engine) emitExtractionEvent(stage, phase, client, detail string) {
	if e == nil || e.config.OnExtractionEvent == nil {
		return
	}
	e.config.OnExtractionEvent(innertube.ExtractionEvent{
		Stage:  stage,
		Phase:  phase,
		Client: client,
		Detail: detail,
	})
}
