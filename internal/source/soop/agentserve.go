package soop

// Detached serve daemon for the agent stream. MPV's ytdl_hook (and any resolver
// that reads `-J` JSON) plays the returned URL AFTER the resolving process exits,
// so the loopback server that produces the live MPEG-TS must outlive it. Instead
// of serving in-process, serveAgentStream spawns a separate `ytv1 soopserve`
// process (RunAgentServe) that prints the URL, serves it, and exits when the
// player disconnects or never connects. A `ytv1 <url> -o file` download uses the
// same detached server; it just consumes the stream itself.

import (
	"bufio"
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

// agentHandler builds the loopback HTTP handler that muxes the agent's live media
// to MPEG-TS on the first plain GET. A Range/HEAD probe gets 200/Accept-Ranges:none
// so the downloader falls back to a single plain GET. st (may be nil) tracks the
// connection lifecycle for the daemon's auto-shutdown.
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
	srv := &http.Server{Handler: s.agentHandler(p, st)}
	go func() { _ = srv.Serve(ln) }()

	// The URL line on stdout is the daemon's only stdout output; the parent reads
	// exactly this line.
	fmt.Printf("http://%s/live.ts\n", ln.Addr().String())

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
	cmd.Stderr = os.Stderr
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
