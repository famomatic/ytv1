package downloader

import "net/http"

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

func applyRequestHeaders(req *http.Request, headers http.Header) {
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
}
