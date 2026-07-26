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
	"time"

	"github.com/famomatic/ytv1/internal/timeshift"
)

// timeshiftEnabled reports whether the agent stream is served as a seekable HLS
// DVR rather than a single linear MPEG-TS stream.
func timeshiftEnabled() bool {
	switch os.Getenv("YTV1_SOOP_TIMESHIFT") {
	case "1", "true", "TRUE", "yes":
		return true
	}
	return false
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
	dvr := timeshift.NewDVR(timeshift.Config{}) // defaults: 3s segments, 2-min window
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
