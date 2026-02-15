package formats

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// HLSManifest represents a parsed HLS manifest (placeholder).
type HLSManifest struct {
	RawContent string
}

func FetchHLSManifest(ctx context.Context, client *http.Client, url string) (*HLSManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch HLS manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	return &HLSManifest{
		RawContent: string(body),
	}, nil
}
