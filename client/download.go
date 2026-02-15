package client

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DownloadOptions controls stream download behavior.
type DownloadOptions struct {
	Itag       int
	OutputPath string
}

// DownloadResult describes a completed file download.
type DownloadResult struct {
	VideoID    string
	Itag       int
	OutputPath string
	Bytes      int64
}

// Download resolves the selected stream URL and writes it to a local file.
// If options.Itag is 0, the first available format is selected.
// If options.OutputPath is empty, "<videoID>-<itag><ext>" is used.
func (c *Client) Download(ctx context.Context, input string, options DownloadOptions) (*DownloadResult, error) {
	videoID, err := normalizeVideoID(input)
	if err != nil {
		return nil, err
	}

	formats, err := c.GetFormats(ctx, videoID)
	if err != nil {
		return nil, err
	}
	if len(formats) == 0 {
		return nil, ErrNoPlayableFormats
	}

	chosen := formats[0]
	if options.Itag != 0 {
		found := false
		for _, f := range formats {
			if f.Itag == options.Itag {
				chosen = f
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, options.Itag)
		}
	}

	streamURL, err := c.ResolveStreamURL(ctx, videoID, chosen.Itag)
	if err != nil {
		return nil, err
	}

	outputPath := options.OutputPath
	if outputPath == "" {
		outputPath = defaultOutputPath(videoID, chosen.Itag, chosen.MimeType)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil && filepath.Dir(outputPath) != "." {
		return nil, err
	}

	out, err := os.Create(outputPath)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	written, err := downloadURLToWriter(ctx, c.config.HTTPClient, streamURL, out)
	if err != nil {
		return nil, err
	}

	return &DownloadResult{
		VideoID:    videoID,
		Itag:       chosen.Itag,
		OutputPath: outputPath,
		Bytes:      written,
	}, nil
}

func downloadURLToWriter(ctx context.Context, httpClient *http.Client, streamURL string, w io.Writer) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed: status=%d", resp.StatusCode)
	}
	return io.Copy(w, resp.Body)
}

func defaultOutputPath(videoID string, itag int, mimeType string) string {
	ext := ".bin"
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		if parts := strings.SplitN(mediaType, "/", 2); len(parts) == 2 && parts[1] != "" {
			ext = "." + parts[1]
		}
	}
	return fmt.Sprintf("%s-%d%s", videoID, itag, ext)
}
