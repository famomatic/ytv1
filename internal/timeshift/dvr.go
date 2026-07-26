package timeshift

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config tunes the DVR window.
type Config struct {
	// TargetSegmentDuration is the nominal length of each HLS segment; the real
	// cut lands on the first keyframe at or after this. Default 3s.
	TargetSegmentDuration time.Duration
	// Window is how much past media to retain (and expose for seeking). Older
	// segments are evicted. Default 2 minutes.
	Window time.Duration
}

// DVR consumes a continuous MPEG-TS stream (it is an io.Writer) and exposes a
// live HLS playlist plus its segments over HTTP, retaining a sliding window so a
// player can seek backward within it. It is source-agnostic: feed it any TS
// stream. Recording (Write, from one goroutine) and serving (Handler, many
// concurrent requests) are decoupled.
type DVR struct {
	cfg Config
	seg *TSSegmenter

	mu    sync.RWMutex
	segs  []Segment     // sliding window, ordered by Seq ascending
	total time.Duration // sum of window segment durations (for eviction)
}

// NewDVR returns a DVR ready to be written to.
func NewDVR(cfg Config) *DVR {
	if cfg.TargetSegmentDuration <= 0 {
		cfg.TargetSegmentDuration = 3 * time.Second
	}
	if cfg.Window <= 0 {
		cfg.Window = 2 * time.Minute
	}
	d := &DVR{cfg: cfg}
	d.seg = NewTSSegmenter(cfg.TargetSegmentDuration, d.addSegment)
	return d
}

// Write feeds MPEG-TS bytes into the segmenter. Safe for arbitrary chunk sizes.
func (d *DVR) Write(p []byte) (int, error) { return d.seg.Write(p) }

// Close flushes the final segment.
func (d *DVR) Close() error {
	d.seg.Close()
	return nil
}

func (d *DVR) addSegment(s Segment) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.segs = append(d.segs, s)
	d.total += s.Duration
	for len(d.segs) > 1 && d.total > d.cfg.Window {
		d.total -= d.segs[0].Duration
		d.segs = d.segs[1:]
	}
}

// SegmentCount returns how many segments are currently in the window.
func (d *DVR) SegmentCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.segs)
}

// Handler serves the HLS playlist at /index.m3u8 and segments at /seg{seq}.ts.
func (d *DVR) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/index.m3u8", d.servePlaylist)
	mux.HandleFunc("/", d.serveSegment)
	return mux
}

func (d *DVR) servePlaylist(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	segs := d.segs
	d.mu.RUnlock()

	var maxDur time.Duration
	for _, s := range segs {
		if s.Duration > maxDur {
			maxDur = s.Duration
		}
	}
	target := int(maxDur.Seconds() + 0.999)
	if target < 1 {
		target = int(d.cfg.TargetSegmentDuration.Seconds()) + 1
	}
	mediaSeq := 0
	if len(segs) > 0 {
		mediaSeq = segs[0].Seq
	}

	var b bytes.Buffer
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", target)
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", mediaSeq)
	for _, s := range segs {
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nseg%d.ts\n", s.Duration.Seconds(), s.Seq)
	}
	// No #EXT-X-ENDLIST: this is a live playlist; the player refetches for new
	// segments and may seek within the ones listed.

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b.Bytes())
}

func (d *DVR) serveSegment(w http.ResponseWriter, r *http.Request) {
	seq, ok := parseSegPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	d.mu.RLock()
	var seg *Segment
	for i := range d.segs {
		if d.segs[i].Seq == seq {
			seg = &d.segs[i]
			break
		}
	}
	d.mu.RUnlock()
	if seg == nil {
		http.NotFound(w, r) // evicted or not yet produced
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	// http.ServeContent adds Accept-Ranges and honours Range requests, so a
	// player can seek within a segment as well as across them.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(seg.Data))
}

// parseSegPath extracts the sequence number from "/seg{seq}.ts".
func parseSegPath(p string) (int, bool) {
	p = strings.TrimPrefix(p, "/")
	if !strings.HasPrefix(p, "seg") || !strings.HasSuffix(p, ".ts") {
		return 0, false
	}
	n, err := strconv.Atoi(p[len("seg") : len(p)-len(".ts")])
	if err != nil {
		return 0, false
	}
	return n, true
}
