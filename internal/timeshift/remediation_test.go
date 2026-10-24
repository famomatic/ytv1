package timeshift

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvictedSlotReleasesMedia(t *testing.T) {
	d := NewDVR(Config{Window: time.Second})
	defer d.Close()
	d.segs = make([]storedSeg, 0, 2)
	d.addSegment(Segment{Seq: 1, Duration: time.Second, Data: []byte("first")})
	backing := d.segs
	d.addSegment(Segment{Seq: 2, Duration: time.Second, Data: []byte("second")})
	// If append reused the original allocation, the evicted slot must be zero.
	if backing[0].data != nil {
		t.Fatal("evicted backing slot retains media")
	}
	if len(d.segs) != 1 || d.segs[0].seq != 2 {
		t.Fatal("wrong retained window")
	}
}

func TestDeferredDeleteRetries(t *testing.T) {
	d := NewDVR(Config{MaxPendingDeletes: 2})
	defer d.Close()
	path := filepath.Join(t.TempDir(), "busy")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(path, "child")
	if err := os.WriteFile(child, []byte("held"), 0600); err != nil {
		t.Fatal(err)
	}
	d.retirePathLocked(path)
	if len(d.pendingDeletes) != 1 {
		t.Fatal("failed deletion not queued")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	d.retryDeletesLocked()
	if len(d.pendingDeletes) != 0 {
		t.Fatal("successful retry not removed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("retired file remains: %v", err)
	}
}
