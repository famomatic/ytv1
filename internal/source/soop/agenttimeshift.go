package soop

// Timeshift (past-seeking) serve mode for the agent stream. Instead of a single
// linear MPEG-TS response, the agent media is recorded into a rolling HLS DVR
// (internal/timeshift) and served as a live playlist + segments, so a player can
// seek backward within the window. Opt-in via YTV1_SOOP_TIMESHIFT=1; the linear
// path (agentHandler) is unchanged when it is off.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/famomatic/ytv1/internal/timeshift"
)

// timeshiftEnabled reports whether the agent stream, when served for playback
// (the detached/resolve path a player opens by URL), is a seekable HLS DVR
// rather than a single linear MPEG-TS stream. Default on; opt out with
// YTV1_SOOP_NO_TIMESHIFT=1. A direct download (`-o …`) always uses the linear
// path regardless, since it wants the raw continuous stream, not HLS.
func timeshiftEnabled() bool {
	switch os.Getenv("YTV1_SOOP_NO_TIMESHIFT") {
	case "1", "true", "TRUE", "yes":
		return false
	}
	return true
}

// dvrConfigFromEnv builds the DVR window/storage config, applying env overrides.
// Segments are kept IN MEMORY by default: a bounded window is only a few hundred
// MB (2 min of 1440p60 ≈ 300 MB), which is trivial next to the RAM cost of
// hammering a possibly-slow or busy temp SSD with the per-segment write +
// read-back + eviction-delete churn — that saturated a budget NVMe (100% active,
// ~6 s response) and stalled playback. Opt into disk spill (for a very long
// window on a RAM-constrained box) by setting YTV1_TIMESHIFT_DIR to a directory.
func dvrConfigFromEnv() timeshift.Config {
	cfg := timeshift.Config{}
	if v := envSeconds("YTV1_TIMESHIFT_WINDOW_SECS"); v > 0 {
		cfg.Window = v
	}
	if v := envSeconds("YTV1_TIMESHIFT_SEG_SECS"); v > 0 {
		cfg.TargetSegmentDuration = v
	}
	if d := os.Getenv("YTV1_TIMESHIFT_DIR"); d != "" {
		cfg.Dir = d // explicit opt-in to on-disk segment spill
	}
	return cfg
}

func envSeconds(name string) time.Duration {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 0
}

// dvrServer records the agent stream into a timeshift DVR and serves its HLS
// endpoints (/index.m3u8 + /seg{n}.ts). Recording starts immediately and runs
// until ctx is cancelled, so segments are ready when a player first fetches the
// playlist.
type dvrServer struct {
	dvr *timeshift.DVR
	st  *agentServeState
}

// newDVRServer starts recording p into a fresh DVR and returns an HTTP handler
// for it. Cancel ctx to stop recording and release the agent session.
func (s *Source) newDVRServer(ctx context.Context, p agentStreamParams, st *agentServeState) *dvrServer {
	dvr := timeshift.NewDVR(dvrConfigFromEnv())
	go func() {
		if err := s.streamAgentMedia(ctx, p, dvr); err != nil {
			fmt.Fprintf(os.Stderr, "soop: agent DVR recording ended: %v\n", err)
		}
		_ = dvr.Close()
	}()
	return &dvrServer{dvr: dvr, st: st}
}

func (ds *dvrServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if ds.st != nil {
		ds.st.touch()
	}
	// Make the first playlist fetch wait briefly for the first segment so the
	// player never sees an empty live playlist (which some demuxers reject).
	if r.URL.Path == "/index.m3u8" {
		deadline := time.Now().Add(8 * time.Second)
		for ds.dvr.SegmentCount() == 0 && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
	}
	ds.dvr.Handler().ServeHTTP(w, r)
}
