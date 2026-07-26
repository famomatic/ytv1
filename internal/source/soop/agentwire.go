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

// serveAgentStream starts a loopback HTTP server that muxes the agent's live
// media to MPEG-TS and streams it (incrementally) to the pipeline. It returns the
// URL the pipeline downloads. The stream is driven once, on the plain GET; a
// Range/HEAD probe gets 200/Accept-Ranges:none so the downloader falls back to
// that single plain GET. A per-BNO cache keeps Extract's metadata and download
// passes on the same URL (and one agent session).
func (s *Source) serveAgentStream(p agentStreamParams) (string, error) {
	agentStreamMu.Lock()
	defer agentStreamMu.Unlock()
	if u, ok := agentStreamByBNO[p.BNO]; ok {
		return u, nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	var once sync.Once
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "none")
		w.Header().Set("Content-Type", "video/mp2t")
		if r.Method != http.MethodGet || r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		streamed := false
		once.Do(func() {
			streamed = true
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
	})}
	go func() { _ = srv.Serve(ln) }()
	streamURL := fmt.Sprintf("http://%s/live.ts", ln.Addr().String())
	agentStreamByBNO[p.BNO] = streamURL
	return streamURL, nil
}

// buildAgentMedia assembles the Media whose single format is the loopback agent
// stream (a direct MPEG-TS download).
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
			MimeType:     "video/mp2t",
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
