package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/mjmst/ytv1/internal/innertube"
	"github.com/mjmst/ytv1/internal/policy"
	"github.com/mjmst/ytv1/internal/types"
)

// Engine is the main orchestrator for video extraction.
type Engine struct {
	selector policy.Selector
	config   innertube.Config
}

func NewEngine(selector policy.Selector, config innertube.Config) *Engine {
	return &Engine{
		selector: selector,
		config:   config,
	}
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
	if len(clients) == 0 {
		return nil, types.ErrNoClientsAvailable
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan extractionResult, len(clients))
	var wg sync.WaitGroup

	for _, profile := range clients {
		wg.Add(1)
		go func(p innertube.ClientProfile) {
			defer wg.Done()
			
			// Check for context cancellation before starting
			if ctx.Err() != nil {
				return
			}

			// TODO: Inject PO Token if required
			// TODO: Use specific HTTP client / Proxy from config

			req := innertube.NewPlayerRequest(p, videoID)
			
			resp, err := e.fetch(ctx, req, p)
			
			select {
			case results <- extractionResult{response: resp, err: err, client: p.Name}:
			case <-ctx.Done():
			}
		}(profile)
	}

	// Wait for first success or all failures
	// This is a simplified version. A more robust one handles "all failed" properly.
	
	// Closer for the results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	var attempts []AttemptError
	for res := range results {
		if res.err == nil {
			cancel() // Cancel other requests
			return res.response, nil
		}
		attempts = append(attempts, AttemptError{
			Client: res.client,
			Err:    res.err,
		})
	}

	if len(attempts) > 0 {
		return nil, &AllClientsFailedError{Attempts: attempts}
	}
	return nil, types.ErrNoClientsAvailable
}

func (e *Engine) fetch(ctx context.Context, req *innertube.PlayerRequest, profile innertube.ClientProfile) (*innertube.PlayerResponse, error) {
	// Construct URL
	url := "https://" + profile.Host + "/youtubei/v1/player?key=" + profile.APIKey
	if profile.APIKey == "" {
		// Default API key for Web if not present (simplified)
		// Usually profiles have keys. If not, we might need a default or extract from JS.
		// For now, let's assume valid profiles or use a known default.
		url = "https://" + profile.Host + "/youtubei/v1/player"
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
