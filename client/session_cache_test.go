package client

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestSessionCacheTTLExpiresEntries(t *testing.T) {
	c := &Client{
		config: Config{
			SessionCacheTTL: 5 * time.Millisecond,
		},
		sessions: make(map[string]videoSession),
	}
	c.putSession("a", videoSession{
		Response: &innertube.PlayerResponse{
			VideoDetails: innertube.VideoDetails{VideoID: "a"},
		},
	})

	time.Sleep(15 * time.Millisecond)
	if _, ok := c.getSession("a"); ok {
		t.Fatalf("expected session to expire by ttl")
	}
	if len(c.sessions) != 0 {
		t.Fatalf("expected expired session to be removed, len=%d", len(c.sessions))
	}
}

func TestSessionCacheMaxEntriesEvictsLRU(t *testing.T) {
	c := &Client{
		config: Config{
			SessionCacheMaxEntries: 2,
		},
		sessions: make(map[string]videoSession),
	}

	c.putSession("a", videoSession{Response: &innertube.PlayerResponse{}})
	time.Sleep(2 * time.Millisecond)
	c.putSession("b", videoSession{Response: &innertube.PlayerResponse{}})
	time.Sleep(2 * time.Millisecond)
	if _, ok := c.getSession("a"); !ok {
		t.Fatalf("expected session a to be present")
	}
	time.Sleep(2 * time.Millisecond)
	c.putSession("c", videoSession{Response: &innertube.PlayerResponse{}})

	if _, ok := c.getSession("b"); ok {
		t.Fatalf("expected least-recently-used session b to be evicted")
	}
	if _, ok := c.getSession("a"); !ok {
		t.Fatalf("expected session a to remain")
	}
	if _, ok := c.getSession("c"); !ok {
		t.Fatalf("expected session c to remain")
	}
}

func TestSessionCacheConcurrentAccess_NoPanic(t *testing.T) {
	c := &Client{
		config: Config{
			SessionCacheTTL:        time.Second,
			SessionCacheMaxEntries: 64,
		},
		sessions: make(map[string]videoSession),
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(group int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				id := fmt.Sprintf("v-%d-%d", group, i%80)
				c.putSession(id, videoSession{Response: &innertube.PlayerResponse{}})
				_, _ = c.getSession(id)
			}
		}(g)
	}
	wg.Wait()
	if len(c.sessions) == 0 {
		t.Fatalf("expected sessions to be populated")
	}
}

// sessionWithURLs builds a videoSession whose PlayerResponse carries the
// given format URLs. The expire query param on each URL drives URL-expire
// validation independently of the local TTL.
func sessionWithURLs(urls ...string) videoSession {
	formats := make([]innertube.Format, 0, len(urls))
	for i, u := range urls {
		formats = append(formats, innertube.Format{Itag: 18 + i, URL: u})
	}
	return videoSession{
		Response: &innertube.PlayerResponse{
			StreamingData: innertube.StreamingData{Formats: formats},
		},
	}
}

// TestSessionCacheDefaultTTLExpiresStaleSession asserts that a client with
// no explicit SessionCacheTTL still expires sessions using the 6h default.
// Before the fix, zero meant "never expire".
func TestSessionCacheDefaultTTLExpiresStaleSession(t *testing.T) {
	c := &Client{
		config:   Config{}, // SessionCacheTTL unset -> defaults to 6h
		sessions: make(map[string]videoSession),
	}
	c.putSession("stale", videoSession{
		Response: &innertube.PlayerResponse{},
		// Force CachedAt to well past the default 6h TTL.
	})
	// Rewind CachedAt so the session is older than the default TTL without
	// waiting 6 hours.
	c.sessionsMu.Lock()
	s := c.sessions["stale"]
	s.CachedAt = time.Now().Add(-7 * time.Hour)
	c.sessions["stale"] = s
	c.sessionsMu.Unlock()

	if _, ok := c.getSession("stale"); ok {
		t.Fatalf("expected stale session (older than default 6h TTL) to expire")
	}
	if _, ok := c.sessions["stale"]; ok {
		t.Fatalf("expected expired session to be removed from the map")
	}
}

// TestSessionCacheURLExpireInvalidatesSession asserts that a session whose
// cached format URLs have an `expire` param in the past is treated as
// expired, even when the local TTL is disabled (zero).
func TestSessionCacheURLExpireInvalidatesSession(t *testing.T) {
	c := &Client{
		config: Config{
			SessionCacheTTL: 0, // opt-out local TTL; URL expiry must still apply
		},
		sessions: make(map[string]videoSession),
	}
	expired := time.Now().Add(-1 * time.Hour).Unix()
	fresh := time.Now().Add(5 * time.Hour).Unix()
	c.putSession("expired", sessionWithURLs(
		"https://r1.googlevideo.com/videoplayback?expire="+fmt.Sprintf("%d", expired),
	))
	c.putSession("fresh", sessionWithURLs(
		"https://r2.googlevideo.com/videoplayback?expire="+fmt.Sprintf("%d", fresh),
	))

	if _, ok := c.getSession("expired"); ok {
		t.Fatalf("expected session with expired URLs to be invalidated")
	}
	if _, ok := c.getSession("fresh"); !ok {
		t.Fatalf("expected session with fresh URLs to remain cached")
	}
}

// TestSessionCacheURLExpireSafetyMargin asserts that a URL expiring very
// soon is treated as expired due to the safety margin, avoiding races where
// a URL passes validation but expires during the in-flight request.
func TestSessionCacheURLExpireSafetyMargin(t *testing.T) {
	c := &Client{
		config: Config{
			SessionCacheTTL: time.Hour, // local TTL not the bottleneck here
		},
		sessions: make(map[string]videoSession),
	}
	// Expire 10s in the future: within the 30s safety margin, so it should
	// be treated as already expired.
	soonExpiry := time.Now().Add(10 * time.Second).Unix()
	c.putSession("soon", sessionWithURLs(
		"https://r3.googlevideo.com/videoplayback?expire="+fmt.Sprintf("%d", soonExpiry),
	))

	if _, ok := c.getSession("soon"); ok {
		t.Fatalf("expected URL expiring within safety margin to be treated as expired")
	}
}

// TestSessionCacheNegativeTTLDisablesAgingButURLExpiryApplies asserts the
// opt-out contract: SessionCacheTTL=-1 disables local TTL aging, but
// URL-expire validation still runs.
func TestSessionCacheNegativeTTLDisablesAgingButURLExpiryApplies(t *testing.T) {
	c := &Client{
		config: Config{
			SessionCacheTTL: -1,
		},
		sessions: make(map[string]videoSession),
	}
	// Old CachedAt but no expire param on URLs: TTL disabled so it stays.
	c.putSession("old", videoSession{
		Response: &innertube.PlayerResponse{},
	})
	c.sessionsMu.Lock()
	s := c.sessions["old"]
	s.CachedAt = time.Now().Add(-48 * time.Hour)
	c.sessions["old"] = s
	c.sessionsMu.Unlock()

	if _, ok := c.getSession("old"); !ok {
		t.Fatalf("expected session with no expire param to remain when TTL is disabled")
	}

	// Same session but now with expired URLs: URL expiry must evict it.
	expired := time.Now().Add(-1 * time.Hour).Unix()
	c.putSession("old", sessionWithURLs(
		"https://r4.googlevideo.com/videoplayback?expire="+fmt.Sprintf("%d", expired),
	))
	c.sessionsMu.Lock()
	s2 := c.sessions["old"]
	s2.CachedAt = time.Now().Add(-48 * time.Hour)
	c.sessions["old"] = s2
	c.sessionsMu.Unlock()

	if _, ok := c.getSession("old"); ok {
		t.Fatalf("expected session with expired URLs to be evicted even when TTL is disabled")
	}
}
