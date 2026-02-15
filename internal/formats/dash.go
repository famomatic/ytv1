package formats

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// DASHManifest represents a parsed DASH manifest (placeholder).
type DASHManifest struct {
	RawContent string
}

func FetchDASHManifest(ctx context.Context, client *http.Client, url string) (*DASHManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch DASH manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	return &DASHManifest{
		RawContent: string(body),
	}, nil
}
