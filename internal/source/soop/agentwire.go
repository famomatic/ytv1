package soop

// Wiring that exposes the agent stream (agentstream.go) to the generic pipeline:
// a loopback HTTP server serves the remuxed fragmented MP4, and Extract returns a
// single format pointing at it. The pipeline direct-downloads that URL (Protocol
// is empty, so it takes the plain-HTTP path, not HLS), which drives the agent for
// the download's lifetime.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

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

// auCookie resolves the viewer's _au cookie (the JOINLOG uuid): YTV1_SOOP_AU
// override first, else the shared HTTP client's cookie jar.
func (s *Source) auCookie() string {
	if v := os.Getenv("YTV1_SOOP_AU"); v != "" {
		return v
	}
	if s.http.Jar != nil {
		for _, host := range []string{"https://api.m.sooplive.co.kr", "https://sooplive.co.kr"} {
			if u, err := url.Parse(host); err == nil {
				for _, c := range s.http.Jar.Cookies(u) {
					if c.Name == "_au" {
						return c.Value
					}
				}
			}
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

// serveAgentStream drives the agent in the background, remuxing to a temp file
// (ffmpeg → file never suffers output backpressure), and serves that file to the
// download client by tailing it. It returns the URL the pipeline downloads. The
// stream and server live for the process (the CLI exits after the download).
func (s *Source) serveAgentStream(p agentStreamParams) (string, error) {
	agentStreamMu.Lock()
	defer agentStreamMu.Unlock()
	if u, ok := agentStreamByBNO[p.BNO]; ok {
		return u, nil
	}

	f, err := os.CreateTemp("", "ytv1-soop-*.mp4")
	if err != nil {
		return "", err
	}
	name := f.Name()
	go func() {
		defer f.Close()
		if err := s.streamAgentMedia(context.Background(), p, f); err != nil {
			fmt.Fprintf(os.Stderr, "soop: agent stream ended: %v\n", err)
		}
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Content-Type", "video/mp4")
		if r.Method != http.MethodGet || r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		tailFile(r.Context(), name, w, flusher)
	})}
	go func() { _ = srv.Serve(ln) }()
	streamURL := fmt.Sprintf("http://%s/live.mp4", ln.Addr().String())
	agentStreamByBNO[p.BNO] = streamURL
	return streamURL, nil
}

// tailFile streams a growing file to w until ctx is done, waiting for new bytes.
// Fragmented MP4 has no moov-at-end dependency, so tailing produces a playable
// stream as it is written.
func tailFile(ctx context.Context, name string, w io.Writer, flusher http.Flusher) {
	rf, err := openWhenReady(ctx, name)
	if err != nil {
		return
	}
	defer rf.Close()
	buf := make([]byte, 64<<10)
	idle := 0
	for {
		if ctx.Err() != nil {
			return
		}
		n, err := rf.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			idle = 0
			continue
		}
		if err == io.EOF {
			idle++
			if idle > 6000 { // ~10 min with no growth → give up
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		if err != nil {
			return
		}
	}
}

func openWhenReady(ctx context.Context, name string) (*os.File, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(name); err == nil && fi.Size() > 0 {
			return os.Open(name)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("soop: agent produced no media")
}

// buildAgentMedia assembles the Media whose single format is the loopback agent
// stream (a direct fragmented-MP4 download).
func buildAgentMedia(input, streamURL string, info *liveInfo) *source.Media {
	headers := http.Header{}
	headers.Set("User-Agent", desktopUserAgent)
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
			MimeType:     "video/mp4",
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
