package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DownloadThumbnail downloads the selected thumbnail for info to outputPath.
func (c *Client) DownloadThumbnail(ctx context.Context, info *VideoInfo, outputPath string) error {
	if info == nil || strings.TrimSpace(info.ThumbnailURL) == "" {
		videoID := ""
		if info != nil {
			videoID = info.ID
		}
		return fmt.Errorf("%w: thumbnail unavailable for video=%s", ErrUnavailable, videoID)
	}
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return fmt.Errorf("%w: empty thumbnail output path", ErrInvalidInput)
	}
	dir := filepath.Dir(outputPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create thumbnail directory: %w", err)
		}
	}

	httpClient := http.DefaultClient
	if c != nil && c.HTTPClient() != nil {
		httpClient = c.HTTPClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.ThumbnailURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create thumbnail request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download thumbnail: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("failed to download thumbnail: http status %d", resp.StatusCode)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create thumbnail file: %w", err)
	}
	// Bound the thumbnail size so a malicious or misbehaving URL cannot fill
	// the disk or exhaust memory while streaming to the file. We also drop
	// the partial file on any write/sync/close failure so callers do not see
	// a truncated thumbnail that looks like a successful download.
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxThumbnailBytes)); err != nil {
		f.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("failed to write thumbnail: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(outputPath)
		return fmt.Errorf("failed to sync thumbnail file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(outputPath)
		return fmt.Errorf("failed to close thumbnail file: %w", err)
	}
	return nil
}

// maxThumbnailBytes bounds the size of a downloaded thumbnail to prevent
// unbounded disk/memory consumption from a hostile or malfunctioning URL.
const maxThumbnailBytes int64 = 64 << 20 // 64 MiB
