package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/iox"
)

var ytInitialPlayerResponseMarkers = []string{
	"var ytInitialPlayerResponse = ",
	"window[\"ytInitialPlayerResponse\"] = ",
	"ytInitialPlayerResponse = ",
}

// extractYTInitialPlayerResponse returns the ytInitialPlayerResponse JSON blob
// embedded in a watch page.
func extractYTInitialPlayerResponse(htmlBody []byte) ([]byte, error) {
	body := string(htmlBody)
	for _, marker := range ytInitialPlayerResponseMarkers {
		pos := strings.Index(body, marker)
		if pos < 0 {
			continue
		}
		start := strings.Index(body[pos+len(marker):], "{")
		if start < 0 {
			continue
		}
		start += pos + len(marker)
		if blob, ok := sliceJSONObject(body[start:]); ok {
			return []byte(blob), nil
		}
	}
	return nil, fmt.Errorf("ytInitialPlayerResponse not found")
}

// fillMetadataFromWatchPage mirrors yt-dlp, which always merges the watch
// page's initial player response (metadata only) into the player responses.
// TV clients return streaming data but no videoDetails/microformat/captions,
// so a tv-only extraction would otherwise have no title.
func (c *Client) fillMetadataFromWatchPage(ctx context.Context, videoID string, resp *innertube.PlayerResponse) {
	if resp == nil || strings.TrimSpace(resp.VideoDetails.Title) != "" {
		return
	}
	pageURL := "https://www.youtube.com/watch?v=" + url.QueryEscape(videoID) + "&bpctr=9999999999&has_verified=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", innertube.WebClient.UserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	applyRequestHeaders(req, c.config.RequestHeaders)
	httpResp, err := c.httpClient().Do(req)
	if err != nil {
		c.warnf("metadata watch-page fetch failed: %v", err)
		return
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		return
	}
	body, err := iox.ReadAllLimit(httpResp.Body, 20<<20)
	if err != nil {
		return
	}
	blob, err := extractYTInitialPlayerResponse(body)
	if err != nil {
		return
	}
	var page innertube.PlayerResponse
	if err := json.Unmarshal(blob, &page); err != nil {
		return
	}
	applyWatchPageMetadata(resp, &page)
}

func applyWatchPageMetadata(dst, page *innertube.PlayerResponse) {
	if strings.TrimSpace(dst.VideoDetails.Title) == "" && strings.TrimSpace(page.VideoDetails.Title) != "" {
		dst.VideoDetails = page.VideoDetails
	}
	if reflect.ValueOf(dst.Microformat).IsZero() {
		dst.Microformat = page.Microformat
	}
	if reflect.ValueOf(dst.Captions).IsZero() {
		dst.Captions = page.Captions
	}
}
