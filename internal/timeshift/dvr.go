package timeshift

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config tunes the DVR window and storage.
type Config struct {
	// MaxPendingDeletes bounds deferred file deletions (default 128). When full, new segments use memory until deletions recover.
	MaxPendingDeletes int
	// TargetSegmentDuration is the nominal length of each HLS segment; the real
	// cut lands on the first keyframe at or after this. Default 3s.
	TargetSegmentDuration time.Duration
	MaxSegmentBytes       int64 // caps unfinished segments; default 64 MiB
	// Window is how much past media to retain (and expose for seeking). Older
	// segments are evicted. Default 2 minutes.
	Window time.Duration
	// MaxBytes caps the retained window by size, evicting oldest segments once
	// exceeded — so a high-bitrate stream cannot balloon an in-memory window
	// (2 min of 1440p60 was ~750 MB). 0 means no byte cap. Whichever of Window
	// / MaxBytes is hit first bounds the window.
	MaxBytes int64
	// Dir, if non-empty, spills segments to files under a fresh subdirectory of
	// Dir instead of holding them in memory — for long windows without high RAM
	// use. The subdirectory (and its files) are removed on Close. Empty keeps
	// segments in memory.
	Dir string
}

// storedSeg is one retained segment: its bytes live in memory (data) or on disk
// (path), never both.
type storedSeg struct {
	discontinuity bool
	seq           int
	dur           time.Duration
	bytes         int
	data          []byte
	path          string
}

// DVR consumes a continuous MPEG-TS stream (it is an io.Writer) and exposes a
// live HLS playlist plus its segments over HTTP, retaining a sliding window so a
// player can seek backward within it. It is source-agnostic: feed it any TS
// stream. Recording (Write, from one goroutine) and serving (Handler, many
// concurrent requests) are decoupled.
type DVR struct {
	writeMu          sync.Mutex
	finished, closed bool
	ready            chan struct{}
	readyOnce        sync.Once
	handler          http.Handler
	cfg              Config
	seg              *TSSegmenter
	dir              string // the created spill subdirectory (empty for in-memory)

	mu             sync.RWMutex
	pendingDeletes map[string]struct{}
	segs           []storedSeg // sliding window, ordered by seq ascending
	total          time.Duration
	totalBytes     int64
}

func (d *DVR) retirePathLocked(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) && len(d.pendingDeletes) < d.cfg.MaxPendingDeletes {
		d.pendingDeletes[path] = struct{}{}
	}
}

func (d *DVR) retryDeletesLocked() {
	for path := range d.pendingDeletes {
		if err := os.Remove(path); err == nil || os.IsNotExist(err) {
			delete(d.pendingDeletes, path)
		}
	}
}

// NewDVR returns a DVR ready to be written to. If cfg.Dir is set but a spill
// subdirectory cannot be created, it falls back to in-memory storage.
func NewDVR(cfg Config) *DVR {
	if cfg.TargetSegmentDuration <= 0 {
		cfg.TargetSegmentDuration = 3 * time.Second
	}
	if cfg.Window <= 0 {
		cfg.Window = 2 * time.Minute
	}
	if cfg.MaxPendingDeletes <= 0 {
		cfg.MaxPendingDeletes = 128
	}
	d := &DVR{cfg: cfg, ready: make(chan struct{}), pendingDeletes: make(map[string]struct{})}
	if cfg.Dir != "" {
		if dir, err := os.MkdirTemp(cfg.Dir, "ytv1-dvr-"); err == nil {
			d.dir = dir
		}
	}
	d.seg = NewTSSegmenter(cfg.TargetSegmentDuration, d.addSegment)
	limit := cfg.MaxSegmentBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	if cfg.MaxBytes > 0 && cfg.MaxBytes < limit {
		limit = cfg.MaxBytes
	}
	d.seg.MaxSegmentBytes = limit
	mux := http.NewServeMux()
	mux.HandleFunc("/index.m3u8", d.servePlaylist)
	mux.HandleFunc("/", d.serveSegment)
	d.handler = mux
	return d
}

// Write feeds MPEG-TS bytes into the segmenter. Safe for arbitrary chunk sizes.
func (d *DVR) Write(p []byte) (int, error) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.finished || d.closed {
		return 0, io.ErrClosedPipe
	}
	return d.seg.Write(p)
}

// Finish stops recording, retaining the final playlist and spill files for playback.
func (d *DVR) Finish() {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.finished {
		return
	}
	d.seg.Close()
	d.mu.Lock()
	d.finished = true
	d.mu.Unlock()
	d.readyOnce.Do(func() { close(d.ready) })
}

// Close releases retained media after HTTP serving has stopped.
func (d *DVR) Close() error {
	d.Finish()
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	d.segs = nil
	if d.dir != "" {
		return os.RemoveAll(d.dir)
	}
	return nil
}

func (d *DVR) WaitReady(ctx context.Context) error {
	select {
	case <-d.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *DVR) addSegment(s Segment) {
	ss := storedSeg{discontinuity: s.Discontinuity, seq: s.Seq, dur: s.Duration, bytes: len(s.Data)}
	d.mu.RLock()
	spill := len(d.pendingDeletes) < d.cfg.MaxPendingDeletes
	d.mu.RUnlock()
	if d.dir != "" && spill {
		p := filepath.Join(d.dir, fmt.Sprintf("seg%d.ts", s.Seq))
		if err := os.WriteFile(p, s.Data, 0o644); err == nil {
			ss.path = p
		} else {
			ss.data = s.Data // fall back to memory for this segment
		}
	} else {
		ss.data = s.Data
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.retryDeletesLocked()
	d.segs = append(d.segs, ss)
	d.readyOnce.Do(func() { close(d.ready) })
	d.total += ss.dur
	d.totalBytes += int64(ss.bytes)
	// Evict oldest while over EITHER the duration window or the byte cap (keeping
	// at least one segment).
	for len(d.segs) > 1 && (d.total > d.cfg.Window || (d.cfg.MaxBytes > 0 && d.totalBytes > d.cfg.MaxBytes)) {
		old := d.segs[0]
		d.total -= old.dur
		d.totalBytes -= int64(old.bytes)
		d.segs[0] = storedSeg{}
		d.segs = d.segs[1:]
		if old.path != "" {
			d.retirePathLocked(old.path)
		}
	}
}

// SegmentCount returns how many segments are currently in the window.
func (d *DVR) SegmentCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.segs)
}

// Handler serves the HLS playlist at /index.m3u8 and segments at /seg{seq}.ts.
func (d *DVR) Handler() http.Handler { return d.handler }

func (d *DVR) servePlaylist(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	segs := append([]storedSeg(nil), d.segs...)
	finished, closed := d.finished, d.closed
	d.mu.RUnlock()

	if closed {
		http.Error(w, "DVR closed", http.StatusGone)
		return
	}
	var maxDur time.Duration
	for _, s := range segs {
		if s.dur > maxDur {
			maxDur = s.dur
		}
	}
	target := int(maxDur.Seconds() + 0.999)
	if target < 1 {
		target = int(d.cfg.TargetSegmentDuration.Seconds()) + 1
	}
	mediaSeq := 0
	if len(segs) > 0 {
		mediaSeq = segs[0].seq
	}

	var b bytes.Buffer
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", target)
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:%d\n", mediaSeq)
	for _, s := range segs {
		if s.discontinuity {
			b.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nseg%d.ts\n", s.dur.Seconds(), s.seq)
	}
	if finished {
		b.WriteString("#EXT-X-ENDLIST\n")
	}
	// A finished DVR keeps the final playlist until Close releases its storage.

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
	var found storedSeg
	var ok2 bool
	i := sort.Search(len(d.segs), func(i int) bool { return d.segs[i].seq >= seq })
	if i < len(d.segs) && d.segs[i].seq == seq {
		found = d.segs[i]
		ok2 = true
	}
	var file *os.File
	var openErr error
	if ok2 && found.path != "" {
		file, openErr = os.Open(found.path)
	}
	d.mu.RUnlock()
	if openErr != nil {
		http.NotFound(w, r)
		return
	}
	if file != nil {
		defer func() { file.Close(); d.mu.Lock(); d.retryDeletesLocked(); d.mu.Unlock() }()
	}
	if !ok2 {
		http.NotFound(w, r) // evicted or not yet produced
		return
	}
	w.Header().Set("Content-Type", "video/mp2t")
	// ServeContent/ServeFile add Accept-Ranges and honour Range, so a player can
	// seek within a segment as well as across them.
	if found.path != "" {
		http.ServeContent(w, r, "", time.Time{}, file)
		return
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(found.data))
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
