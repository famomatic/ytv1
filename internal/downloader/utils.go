package downloader

import (
	"net/url"
)

func resolveURL(base, ref string) string {
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

// maxSeenSegments bounds the size of the seenSegments dedup map so that a
// long-running live capture does not leak memory indefinitely. Once the map
// exceeds this size the whole map is reset; the lastSeq comparison still
// prevents re-downloading segments within the current live window, and the
// map rebuilds quickly on subsequent successful downloads.
const maxSeenSegments = 4096

// trackSeen inserts url into m and, if the map has grown beyond
// maxSeenSegments, resets it to a fresh map containing only url. Callers rely
// on lastSeq for primary dedup, so the map only needs to cover the current
// playlist window.
func trackSeen(m map[string]bool, url string) map[string]bool {
	if m == nil {
		m = make(map[string]bool)
	}
	m[url] = true
	if len(m) > maxSeenSegments {
		m = make(map[string]bool, 1)
		m[url] = true
	}
	return m
}
