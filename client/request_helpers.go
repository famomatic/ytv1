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
	if _, ok := ctx.Deadline(); ok {
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
	applyRequestHeaders(req, headers)

	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", innertube.WebClient.UserAgent)
	}
	if req.Header.Get("Origin") == "" {
		req.Header.Set("Origin", "https://www.youtube.com")
	}
	if req.Header.Get("Referer") == "" {
		if videoID != "" {
			req.Header.Set("Referer", "https://www.youtube.com/watch?v="+videoID)
			return
		}
		req.Header.Set("Referer", "https://www.youtube.com/")
	}
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
