package soop

// Detached serve daemon for the agent stream. MPV's ytdl_hook (and any resolver
// that reads `-J` JSON) plays the returned URL AFTER the resolving process exits,
// so on a resolve-only run the loopback server that produces the live MPEG-TS
// must outlive it: serveAgentStream spawns a separate `ytv1 soopserve` process
// (RunAgentServe) that prints the URL, serves it, and exits when the player
// disconnects or never connects. A `ytv1 <url> -o …` download instead serves
// in-process (see serveAgentStream / detachServe) so the server dies with the
// process — a detached daemon there could outlive a wedged consumer pipe and keep
// pulling from the local agent forever.

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// agentServeState tracks live connections so the daemon can self-terminate.
type agentServeState struct {
	mu            sync.Mutex
	active        int
	everConnected bool
	lastActive    time.Time
}

func (st *agentServeState) enter() {
	st.mu.Lock()
	st.active++
	st.everConnected = true
	st.lastActive = time.Now()
	st.mu.Unlock()
}

func (st *agentServeState) leave() {
	st.mu.Lock()
	st.active--
	st.lastActive = time.Now()
	st.mu.Unlock()
}

// touch records client activity without holding a persistent connection — used
// by the HLS DVR path, whose requests are short and frequent rather than one
// long stream. The auto-shutdown loop then treats a gap in requests as the
// player having gone away.
func (st *agentServeState) touch() {
	st.mu.Lock()
	st.everConnected = true
	st.lastActive = time.Now()
	st.mu.Unlock()
}

// agentHandler builds the loopback HTTP handler that muxes the agent's live media
// to MPEG-TS on the first plain GET. This linear endpoint is consumed by ytv1's
// own downloader (`-o file`/`-o -`), which probes with Range/HEAD before the real
// GET; those get 200/Accept-Ranges:none with no body so the downloader falls back
// to one plain GET (the single agent session is not consumed by a probe). Players
// that want to seek use the HLS DVR (agenttimeshift.go), not this endpoint. st
// (may be nil) tracks the connection lifecycle for auto-shutdown.
func (s *Source) agentHandler(p agentStreamParams, st *agentServeState) http.HandlerFunc {
	var once sync.Once
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Content-Type", "video/mp2t")
		if r.Method != http.MethodGet || r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		streamed := false
		once.Do(func() {
			streamed = true
			if st != nil {
				st.enter()
				defer st.leave()
			}
			w.WriteHeader(http.StatusOK)
			fw := flushWriter{w: w}
			fw.f, _ = w.(http.Flusher)
			if err := s.streamAgentMedia(r.Context(), p, fw); err != nil {
				fmt.Fprintf(os.Stderr, "soop: agent stream ended: %v\n", err)
			}
		})
		if !streamed {
			http.Error(w, "stream already in progress", http.StatusConflict)
		}
	}
}

// RunAgentServe is the `soopserve` subcommand body: it decodes the base64 JSON
// session params, prints the loopback stream URL to stdout, serves the live
// MPEG-TS, and returns (exits) when the client disconnects or never connects.
func RunAgentServe(encodedParams string) error {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encodedParams))
	if err != nil {
		return err
	}
	var p agentStreamParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	s := New(nil)
	st := &agentServeState{lastActive: time.Now()}

	// In timeshift mode, serve a seekable HLS DVR; otherwise the single linear
	// MPEG-TS. Cancelling recordCtx on shutdown stops recording and releases the
	// agent session.
	recordCtx, cancelRecord := context.WithCancel(context.Background())
	defer cancelRecord()
	var handler http.Handler
	streamPath := "live.ts"
	if timeshiftEnabled() {
		handler = s.newDVRServer(recordCtx, p, st)
		streamPath = "index.m3u8"
	} else {
		handler = s.agentHandler(p, st)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()

	// The URL line on stdout is the daemon's only stdout output; the parent reads
	// exactly this line.
	fmt.Printf("http://%s/%s\n", ln.Addr().String(), streamPath)

	start := time.Now()
	for {
		time.Sleep(2 * time.Second)
		st.mu.Lock()
		active, ever, last := st.active, st.everConnected, st.lastActive
		st.mu.Unlock()
		if active > 0 {
			continue
		}
		if !ever {
			if time.Since(start) > 90*time.Second {
				return nil // nobody ever connected — orphaned resolve
			}
		} else if time.Since(last) > 15*time.Second {
			return nil // client disconnected and did not return
		}
	}
}

// spawnDetachedServe starts a `ytv1 soopserve` process that outlives this one and
// returns the loopback URL it prints. The child keeps running after the parent
// exits (no job object), so a resolver can hand the URL to a player.
func spawnDetachedServe(p agentStreamParams) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	pj, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exe, "soopserve", base64.StdEncoding.EncodeToString(pj))
	// The daemon must NOT inherit this process's stderr: a resolver like MPV's
	// ytdl_hook captures the child's stdout+stderr and waits for both pipes to
	// reach EOF. If the detached daemon held a copy of that stderr pipe, it would
	// never close (the daemon outlives us), so MPV would hang at resolve. Send the
	// daemon's stderr to its own log file instead.
	if lf, e := os.CreateTemp("", "ytv1-soopserve-*.log"); e == nil {
		cmd.Stderr = lf
		defer lf.Close() // the child keeps its own dup after Start
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		return "", err
	}
	go func() { _ = cmd.Wait() }() // reap without blocking; child outlives us
	url := strings.TrimSpace(line)
	if !strings.HasPrefix(url, "http://") {
		return "", fmt.Errorf("soop: soopserve returned no url")
	}
	return url, nil
}
