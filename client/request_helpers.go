package client

import (
	"context"
	"net/http"
	"time"

	"github.com/famomatic/ytv1/internal/innertube"
)

func withDefaultTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func applyRequestHeaders(req *http.Request, headers http.Header) {
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
}

func applyMediaRequestHeaders(req *http.Request, headers http.Header, videoID string) {
	merged := buildMediaRequestHeaders(headers, videoID)
	applyRequestHeaders(req, merged)
}

func applyMediaRequestHeadersForSourceClient(req *http.Request, headers http.Header, videoID string, sourceClient string) {
	merged := buildMediaRequestHeadersForSourceClient(headers, videoID, sourceClient)
	applyRequestHeaders(req, merged)
}

func cloneHeader(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := make(http.Header, len(h))
	for k, vals := range h {
		cp := make([]string, len(vals))
		copy(cp, vals)
		out[k] = cp
	}
	return out
}

func buildMediaRequestHeaders(headers http.Header, videoID string) http.Header {
	return buildMediaRequestHeadersForSourceClient(headers, videoID, "")
}

func buildMediaRequestHeadersForSourceClient(headers http.Header, videoID string, sourceClient string) http.Header {
	merged := cloneHeader(headers)
	if merged == nil {
		merged = make(http.Header)
	}

	if merged.Get("User-Agent") == "" {
		merged.Set("User-Agent", mediaUserAgentForSourceClient(sourceClient))
	}
	if merged.Get("Origin") == "" {
		merged.Set("Origin", "https://www.youtube.com")
	}
	if merged.Get("Referer") == "" {
		if videoID != "" {
			merged.Set("Referer", "https://www.youtube.com/watch?v="+videoID)
		} else {
			merged.Set("Referer", "https://www.youtube.com/")
		}
	}

	return merged
}

func mediaUserAgentForSourceClient(sourceClient string) string {
	if profile, ok := resolveSourceClientProfile(sourceClient); ok && profile.UserAgent != "" {
		return profile.UserAgent
	}
	return innertube.WebClient.UserAgent
}

func mergeHeaders(dst http.Header, src http.Header) {
	if src == nil {
		return
	}
	if dst == nil {
		return
	}
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}
