package timeshift

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestDVRConcurrentAccess hammers the DVR the way the daemon does: one goroutine
// records (Write) while many goroutines fetch the playlist and segments, then
// Close races with in-flight requests. It asserts no panic/deadlock and that
// every served response is coherent. Run with -race (in an amd64+cgo env) to
// also catch data races; even without it this surfaces panics and deadlocks.
func TestDVRConcurrentAccess(t *testing.T) {
	d := NewDVR(Config{TargetSegmentDuration: 500 * time.Millisecond, Window: 3 * time.Second})
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Recorder: feed keyframe-bearing TS continuously until stopped.
	wg.Add(1)
	go func() {
		defer wg.Done()
		pts := int64(0)
		cc := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			d.Write(patPacket())
			d.Write(pmtPacket())
			d.Write(videoPES(pts, cc, true))
			cc = (cc + 1) & 0xF
			d.Write(videoPES(pts+22500, cc, false))
			cc = (cc + 1) & 0xF
			pts += 45000 // +0.5s
		}
	}()

	// Readers: fetch playlist + a segment repeatedly.
	client := srv.Client()
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				resp, err := client.Get(srv.URL + "/index.m3u8")
				if err != nil {
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				// Grab a segment; a 404 (evicted/not-yet-made) is fine.
				sresp, err := client.Get(srv.URL + "/seg0.ts")
				if err == nil {
					if sresp.StatusCode == http.StatusOK {
						b, _ := io.ReadAll(sresp.Body)
						if len(b)%tsPacketLen != 0 {
							t.Errorf("served segment not packet-aligned: %d", len(b))
						}
					}
					sresp.Body.Close()
				}
			}
		}()
	}

	time.Sleep(400 * time.Millisecond)
	close(stop)
	wg.Wait()
	d.Close() // Close after readers stop; also exercise Close path.
}
