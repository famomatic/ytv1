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
