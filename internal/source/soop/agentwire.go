package soop

// Wiring that exposes the agent stream (agentstream.go) to the generic pipeline:
// a loopback HTTP server serves the remuxed MPEG-TS, and Extract returns a single
// format pointing at it. The pipeline direct-downloads that URL (Protocol is
// empty, so it takes the plain-HTTP path, not HLS), which drives the agent for
// the download's lifetime.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/famomatic/ytv1/internal/source"
	"github.com/famomatic/ytv1/internal/types"
)

// agentStreamCache dedups agent sessions: the pipeline extracts a URL more than
// once (metadata then download), and each join to the local agent would take the
// same session port and conflict. Keyed by broadcast number, the first call
// starts the stream+server and later calls reuse the URL.
type agentEndpoint struct {
	url     string
	expires time.Time
	close   func()
}

// Endpoint ownership is per Source, including credentials and server lifecycle.

// watchAPI mints the broadcast fan_ticket the agent gateway validates, and also
// returns authoritative gateway/relay coordinates. Var for tests.
var watchAPI = "https://api.m.sooplive.co.kr/broad/a/watch"

// newAgentGUID returns a random 32-char uppercase-hex client GUID (the browser
// uses a per-device value of this shape).
func newAgentGUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "25631A3AB79CB882B26207735783A003"
	}
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

// fetchAU resolves the anonymous viewer's _au cookie (the P2P JOINLOG uuid).
// SOOP issues it via Set-Cookie to any visitor — no login — so ytv1 fetches it
// itself: YTV1_SOOP_AU override, then a logged-in cookie jar (if --cookies gave
// one), else a GET of the play page whose response sets a fresh _au.
func (s *Source) fetchAU(ctx context.Context, bjID string) string {
	if v := os.Getenv("YTV1_SOOP_AU"); v != "" {
		return v
	}
	if s.http.Jar != nil {
		for _, host := range []string{"https://api.m.sooplive.co.kr", "https://sooplive.co.kr"} {
			if u, err := url.Parse(host); err == nil {
				for _, c := range s.http.Jar.Cookies(u) {
					if c.Name == "_au" && c.Value != "" {
						return c.Value
					}
				}
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://play.sooplive.com/"+bjID, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", desktopUserAgent)
	resp, err := s.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "_au" && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

// watchInfo is the subset of /broad/a/watch we consume.
type watchInfo struct {
	FanTicket   string
	GateWayIP   string
	GateWayPort string
	RelayIP     string
	RelayPort   string
}

// fetchWatchInfo POSTs /broad/a/watch (Cookie _au) for the html5 fan_ticket the
// gateway validates, plus gateway/relay coordinates.
func (s *Source) fetchWatchInfo(ctx context.Context, bjID, bno, au string) (*watchInfo, error) {
	form := url.Values{}
	form.Set("bj_id", bjID)
	form.Set("broad_no", bno)
	form.Set("player_type", "html5")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, watchAPI, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", desktopUserAgent)
	if au != "" {
		req.Header.Set("Cookie", "_au="+au)
	}
	body, err := s.do(req)
	if err != nil {
		return nil, fmt.Errorf("soop: watch api: %w", err)
	}
	var res struct {
		Data struct {
			FanTicket   string      `json:"fan_ticket"`
			GateWayIP   string      `json:"gateway_ip"`
			GateWayPort json.Number `json:"gateway_port"`
			RelayIP     string      `json:"relay_ip"`
			RelayPort   json.Number `json:"relay_port"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("soop: decode watch: %w", err)
	}
	if res.Data.FanTicket == "" {
		return nil, fmt.Errorf("soop: watch returned no fan_ticket")
	}
	return &watchInfo{
		FanTicket:   res.Data.FanTicket,
		GateWayIP:   res.Data.GateWayIP,
		GateWayPort: res.Data.GateWayPort.String(),
		RelayIP:     res.Data.RelayIP,
		RelayPort:   res.Data.RelayPort.String(),
	}, nil
}

// flushWriter flushes an http.ResponseWriter after each write so the muxed
// MPEG-TS reaches the download client as it is produced (the stream is live and
// has no end).
type flushWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (fw flushWriter) Write(b []byte) (int, error) {
	n, err := fw.w.Write(b)
	if fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}

// detachServe selects the detached-daemon server over the in-process one. It is
// only correct when this process resolves a URL and then EXITS before the media
// is consumed (metadata/`-J`/`-g`/simulate runs, where MPV's ytdl_hook plays the
// URL after ytv1 exits). For an actual download (`-o`) this process lives for the
// whole stream, so the in-process server — which dies with it — is used instead;
// a detached daemon there could outlive a wedged pipe (e.g. MPV closing behind a
// shell pipe that hides the broken-pipe) and keep pulling from the agent forever.
// main sets this from the CLI options before extraction.
var detachServe atomic.Bool

// SetDetachServe records whether the current run only resolves a URL and exits
// (true) versus downloads the media itself (false). See detachServe.
func SetDetachServe(v bool) { detachServe.Store(v) }

// serveAgentStream returns a loopback URL that streams the agent's live media as
// MPEG-TS. In resolve-only runs it uses a detached `ytv1 soopserve` process so
// the URL stays playable after this process exits; otherwise (a download) it
// serves in-process so the server dies when this process does. A per-BNO cache
// keeps Extract's metadata and download passes on one URL and one agent session.
func (s *Source) serveAgentStream(p agentStreamParams) (string, error) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.closed {
		return "", fmt.Errorf("source closed")
	}
	if s.streams == nil {
		s.streams = make(map[string]agentEndpoint)
	}
	if e, ok := s.streams[p.BNO]; ok && (e.expires.IsZero() || time.Now().Before(e.expires)) {
		return e.url, nil
	}
	if old, ok := s.streams[p.BNO]; ok {
		delete(s.streams, p.BNO)
		if old.close != nil {
			go old.close()
		}
	}
	// NOTE: no pre-flight media probe here. SOOPStreamer serves a single session
	// per broadcast, so probing would consume that session and the daemon's
	// (second) join then gets no media — breaking the working path. The trade-off
	// is that a stream-time agent failure (e.g. the browser tab is open) is not
	// auto-fallen-back to the CDN; force the CDN with YTV1_SOOP_NO_AGENT=1.
	var url string
	var err error
	if detachServe.Load() {
		// Resolve-only run: a player opens the URL, so serve the seekable HLS DVR
		// (when timeshift is on). The detached daemon does this itself; the
		// in-process fallback mirrors it.
		if url, err = spawnDetachedServe(p); err != nil {
			url, err = s.serveInProcess(p, timeshiftEnabled(), true) // player fallback
		}
	} else {
		// Direct download (`-o …`): serve the raw linear stream this process
		// consumes itself — not HLS, and with downloader Range semantics.
		url, err = s.serveInProcess(p, false, false)
	}
	if err != nil {
		return "", err
	}
	if _, ok := s.streams[p.BNO]; !ok {
		s.streams[p.BNO] = agentEndpoint{url: url, expires: time.Now().Add(90 * time.Second)}
	}
	return url, nil
}

// serveInProcess runs the loopback server in this process (the download path,
// and the fallback when detaching is unavailable). useTimeshift selects the
// seekable HLS DVR over the single linear MPEG-TS; playerMode selects the linear
// endpoint's Range semantics (a media player vs ytv1's own downloader).
func (s *Source) serveInProcess(p agentStreamParams, useTimeshift, playerMode bool) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	recordCtx, cancel := context.WithCancel(context.Background())
	st := &agentServeState{lastActive: time.Now()}
	handler := http.Handler(s.agentHandler(p, st, playerMode))
	streamPath := "live.ts"
	if useTimeshift {
		handler = s.newDVRServer(recordCtx, p, st)
		streamPath = "index.m3u8"
	}
	inner := handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner.ServeHTTP(w, r)
		if !useTimeshift {
			st.mu.Lock()
			ended := st.everConnected && st.active == 0
			st.mu.Unlock()
			if ended {
				s.agentMu.Lock()
				if e, ok := s.streams[p.BNO]; ok {
					e.expires = time.Now()
					s.streams[p.BNO] = e
				}
				s.agentMu.Unlock()
			}
		}
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	url := fmt.Sprintf("http://%s/%s", ln.Addr().String(), streamPath)
	closeServer := func() {
		cancel()
		_ = srv.Close()
		if ds, ok := inner.(*dvrServer); ok {
			ds.Close()
		}
	}
	s.streams[p.BNO] = agentEndpoint{url: url, close: closeServer}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-recordCtx.Done():
				return
			case <-ticker.C:
			}
			st.mu.Lock()
			active, ever, last := st.active, st.everConnected, st.lastActive
			st.mu.Unlock()
			idle := 90 * time.Second
			if ever {
				idle = 15 * time.Second
			}
			if active == 0 && time.Since(last) > idle {
				s.agentMu.Lock()
				if e, ok := s.streams[p.BNO]; ok && e.url == url {
					delete(s.streams, p.BNO)
				}
				s.agentMu.Unlock()
				closeServer()
				return
			}
		}
	}()
	return url, nil
}

// buildAgentMedia assembles the Media whose single format is the loopback agent
// stream: a direct MPEG-TS download, or (timeshift mode) a seekable HLS playlist.
func buildAgentMedia(input, streamURL string, info *liveInfo) *source.Media {
	headers := http.Header{}
	headers.Set("User-Agent", desktopUserAgent)
	mimeType, protocol := "video/mp2t", ""
	if strings.HasSuffix(streamURL, ".m3u8") {
		mimeType, protocol = "application/vnd.apple.mpegurl", "hls"
	}
	return &source.Media{
		ID:           info.BNO,
		Title:        info.Title,
		Author:       info.Author,
		IsLive:       true,
		WebpageURL:   input,
		ThumbnailURL: liveArtworkBase + info.BNO,
		MediaHeaders: headers,
		Formats: []types.FormatInfo{{
			Itag:         1080,
			URL:          streamURL,
			MimeType:     mimeType,
			Protocol:     protocol,
			HasAudio:     true,
			HasVideo:     true,
			Width:        1920,
			Height:       1080,
			Quality:      "original",
			QualityLabel: "1080p (P2P)",
			SourceClient: sourceName,
		}},
	}
}

// MediaURLValid validates loopback endpoint lifetime without probing a one-shot stream.
func (s *Source) MediaURLValid(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Hostname() != "127.0.0.1" {
		return true
	}
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	for _, e := range s.streams {
		if e.url == raw {
			return e.expires.IsZero() || time.Now().Before(e.expires)
		}
	}
	return false
}
func (s *Source) Close() error {
	s.agentMu.Lock()
	s.closed = true
	entries := s.streams
	s.streams = nil
	s.agentMu.Unlock()
	for _, e := range entries {
		if e.close != nil {
			e.close()
		}
	}
	return nil
}
