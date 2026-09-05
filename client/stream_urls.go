package client

import (
	"fmt"
	"net/url"
	"strings"
)

// StreamURLResolver resolves a playable URL for a selected itag.
type StreamURLResolver func(itag int) (string, error)

// SelectedStreamURLs returns direct playable URLs for selected formats, using
// resolver when signatures, throttling parameters, or source token policies apply.
func SelectedStreamURLs(selected []FormatInfo, resolver StreamURLResolver) ([]string, error) {
	if len(selected) == 0 {
		return nil, ErrNoPlayableFormats
	}
	urls := make([]string, 0, len(selected))
	for _, format := range selected {
		u, _ := url.Parse(format.URL)
		needsResolve := format.Ciphered || format.SourceClient != "" || (u != nil && u.Query().Get("n") != "")
		if strings.TrimSpace(format.URL) != "" && !needsResolve {
			urls = append(urls, format.URL)
			continue
		}
		if resolver == nil {
			return nil, fmt.Errorf("%w: direct URL unavailable for itag=%d", ErrUnavailable, format.Itag)
		}
		resolved, err := resolver(format.Itag)
		if err != nil {
			return nil, fmt.Errorf("resolve stream URL for itag=%d: %w", format.Itag, err)
		}
		if strings.TrimSpace(resolved) == "" {
			return nil, fmt.Errorf("%w: empty stream URL for itag=%d", ErrUnavailable, format.Itag)
		}
		urls = append(urls, resolved)
	}
	return urls, nil
}
