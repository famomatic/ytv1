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
	"sync"

	"github.com/famomatic/ytv1/internal/source"
	"github.com/famomatic/ytv1/internal/types"
)

// agentStreamCache dedups agent sessions: the pipeline extracts a URL more than
// once (metadata then download), and each join to the local agent would take the
// same session port and conflict. Keyed by broadcast number, the first call
// starts the stream+server and later calls reuse the URL.
var (
	agentStreamMu    sync.Mutex
	agentStreamByBNO = map[string]string{}
)

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
var detachServe bool

// SetDetachServe records whether the current run only resolves a URL and exits
// (true) versus downloads the media itself (false). See detachServe.
func SetDetachServe(v bool) { detachServe = v }

// serveAgentStream returns a loopback URL that streams the agent's live media as
// MPEG-TS. In resolve-only runs it uses a detached `ytv1 soopserve` process so
// the URL stays playable after this process exits; otherwise (a download) it
// serves in-process so the server dies when this process does. A per-BNO cache
// keeps Extract's metadata and download passes on one URL and one agent session.
func (s *Source) serveAgentStream(p agentStreamParams) (string, error) {
	agentStreamMu.Lock()
	defer agentStreamMu.Unlock()
	if u, ok := agentStreamByBNO[p.BNO]; ok {
		return u, nil
	}
	// NOTE: no pre-flight media probe here. SOOPStreamer serves a single session
	// per broadcast, so probing would consume that session and the daemon's
	// (second) join then gets no media — breaking the working path. The trade-off
	// is that a stream-time agent failure (e.g. the browser tab is open) is not
	// auto-fallen-back to the CDN; force the CDN with YTV1_SOOP_NO_AGENT=1.
	var url string
	var err error
	if detachServe {
		if url, err = spawnDetachedServe(p); err != nil {
			url, err = s.serveInProcess(p) // detach unavailable (e.g. no os.Executable)
		}
	} else {
		url, err = s.serveInProcess(p)
	}
	if err != nil {
		return "", err
	}
	agentStreamByBNO[p.BNO] = url
	return url, nil
}

// serveInProcess runs the loopback server in this process (download path, and
// the fallback when detaching is unavailable). In timeshift mode it serves the
// seekable HLS DVR; otherwise the single linear MPEG-TS.
func (s *Source) serveInProcess(p agentStreamParams) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	handler := http.Handler(s.agentHandler(p, nil))
	streamPath := "live.ts"
	if timeshiftEnabled() {
		handler = s.newDVRServer(context.Background(), p, nil)
		streamPath = "index.m3u8"
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	return fmt.Sprintf("http://%s/%s", ln.Addr().String(), streamPath), nil
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
