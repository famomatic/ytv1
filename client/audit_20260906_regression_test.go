package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/source"
	"github.com/famomatic/ytv1/internal/types"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAudit20260906WrongChunkRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-2/6")
		w.WriteHeader(206)
		fmt.Fprint(w, "ABC")
	}))
	defer srv.Close()
	f, e := os.Create(filepath.Join(t.TempDir(), "x"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	f.WriteString("abcDEF")
	e = downloadChunkOnce(context.Background(), srv.Client(), srv.URL, f, 3, 5, "", nil, nil)
	b := make([]byte, 6)
	f.ReadAt(b, 0)
	if e == nil || string(b) != "abcDEF" {
		t.Fatalf("regression: %q %v", b, e)
	}

}
func TestAudit20260906OversizedChunkOverwritesNeighbor(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Range", "bytes 0-2/6")
		w.WriteHeader(206)
		if calls == 1 {
			fmt.Fprint(w, "abcdef")
		} else {
			fmt.Fprint(w, "ABC")
		}
	}))
	defer srv.Close()
	f, e := os.Create(filepath.Join(t.TempDir(), "x"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	f.WriteString("xxxDEF")
	e = downloadChunkWithRetry(context.Background(), srv.Client(), srv.URL, f, 0, 2, effectiveDownloadTransportConfig{MaxRetries: 1}, "", nil, nil)
	b := make([]byte, 6)
	f.ReadAt(b, 0)
	if e != nil || string(b) != "ABCDEF" {
		t.Fatalf("regression: %q %v", b, e)
	}

}
func TestAudit20260906ShortResumeAccepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 3-4/10")
		w.WriteHeader(206)
		fmt.Fprint(w, "de")
	}))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "x")
	os.WriteFile(p, []byte("abc"), 0600)
	n, e := downloadURLToPath(context.Background(), srv.Client(), srv.URL, p, true, DownloadTransportConfig{})
	if e == nil {
		t.Fatalf("regression: n=%d %v", n, e)
	}

}
func TestAudit20260906PartInstalledBeforeIntegrity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "bad") }))
	defer srv.Close()
	c := New(Config{HTTPClient: srv.Client(), DownloadTransport: DownloadTransportConfig{MaxConcurrency: 1}})
	defer c.Close()
	p := filepath.Join(t.TempDir(), "x")
	os.WriteFile(p, []byte("previous-good-file"), 0600)
	e := c.downloadStream(context.Background(), "abcdefghijk", srv.URL, p, FormatInfo{ContentLength: 20}, false, true)
	b, _ := os.ReadFile(p)
	if e == nil || string(b) != "previous-good-file" {
		t.Fatalf("regression: %q %v", b, e)
	}

}
func TestAudit20260906LiveMergeCancelDeletesRecording(t *testing.T) {
	got := make(chan struct{}, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:5\n#EXTINF:5,\nsegment.ts\n")
		} else {
			fmt.Fprint(w, "recorded-media")
		}
	}))
	defer srv.Close()
	var reported sync.Map
	m := &auditRecordingMuxer{}
	c := New(Config{HTTPClient: srv.Client(), Muxer: m, OnDownloadProgress: func(e DownloadProgressEvent) {
		if e.Downloaded > 0 {
			if _, seen := reported.LoadOrStore(e.Itag, true); !seen {
				got <- struct{}{}
			}
		}
	}})
	defer c.Close()
	fs := []FormatInfo{{Itag: 1, URL: srv.URL + "/v.m3u8", Protocol: "hls", HasVideo: true}, {Itag: 2, URL: srv.URL + "/a.m3u8", Protocol: "hls", HasAudio: true}}
	c.putSession("stub:42", videoSession{SourceName: "stub", Info: &VideoInfo{ID: "42", Formats: fs}})
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cancelDone := make(chan struct{})
	go func() {
		defer close(cancelDone)
		for i := 0; i < 2; i++ {
			select {
			case <-got:
			case <-ctx.Done():
				return
			}
		}
		cancel()
	}()
	_, e := c.downloadAndMerge(ctx, "stub:42", fs, DownloadOptions{OutputPath: filepath.Join(dir, "out.mp4"), UsePartFiles: true}, types.Metadata{})
	<-cancelDone
	entries, _ := os.ReadDir(dir)
	if e == nil || m.called || len(entries) < 2 {
		t.Fatalf("regression: err=%v merged=%v files=%v", e, m.called, entries)
	}

}

type auditRecordingMuxer struct{ called bool }

func (m *auditRecordingMuxer) Available() bool { return true }
func (m *auditRecordingMuxer) Merge(context.Context, string, string, string, types.Metadata) error {
	m.called = true
	return nil
}

func TestAudit20260906ExtractMP3WithExplicitFormat(t *testing.T) {
	o := BuildDownloadOptions(DownloadOptionsRequest{ExtractAudio: true, AudioFormat: "mp3", FormatSelector: "140"})
	if o.Mode != SelectionModeMP3 || o.Itag != 140 {
		t.Fatalf("regression: %+v", o)
	}

}
func TestAudit20260906MergedPreviewAVPlusAudio(t *testing.T) {
	fs := []FormatInfo{{Itag: 18, HasAudio: true, HasVideo: true, MimeType: "video/mp4"}, {Itag: 140, HasAudio: true, MimeType: "audio/mp4"}}
	if !IsMergedSelection(fs) {
		t.Fatal("AV plus separate audio must be a merged selection")
	}

}
func TestAudit20260906HLSIgnoresRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\nseg\n#EXT-X-ENDLIST\n")
		} else {
			fmt.Fprint(w, "0123456789")
		}
	}))
	defer srv.Close()
	c := New(Config{HTTPClient: srv.Client(), DownloadTransport: DownloadTransportConfig{RateLimitBytesPerSecond: 1}})
	defer c.Close()
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "stub:", media: &source.Media{ID: "42", Formats: []FormatInfo{{Itag: 1, URL: srv.URL + "/list", Protocol: "hls", HasAudio: true, HasVideo: true}}}}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, e := c.Download(ctx, "stub:42", DownloadOptions{OutputPath: filepath.Join(t.TempDir(), "out.ts")})
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("regression: %+v %v", r, e)
	}

}

func TestAudit20260906SourceCacheOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "media") }))
	defer srv.Close()
	c := New(Config{HTTPClient: srv.Client(), SessionCacheMaxEntries: 1})
	defer c.Close()
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "stub:", media: &source.Media{ID: "42", Formats: []FormatInfo{{Itag: 1, URL: srv.URL, HasAudio: true, HasVideo: true}}}}}
	body, _, e := c.OpenStream(context.Background(), "stub:42", StreamOptions{Itag: 1})
	if body != nil {
		body.Close()
	}
	if e != nil {
		t.Fatal("regression")
	}

}
func TestAudit20260906ConcurrentArchiveVisibility(t *testing.T) {
	p := filepath.Join(t.TempDir(), "archive")
	a, e := OpenDownloadArchive(p)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := OpenDownloadArchive(p)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if e = a.Add("abcdefghijk"); e != nil {
		t.Fatal(e)
	}
	if !b.Has("abcdefghijk") {
		t.Fatal("regression")
	}

}

func TestAudit20260906ArchiveLongCorruptLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "archive")
	os.WriteFile(p, []byte(strings.Repeat("!", 65537)+"\nabcdefghijk\n"), 0600)
	a, e := OpenDownloadArchive(p)
	if a != nil {
		a.Close()
	}
	if e != nil || !a.Has("abcdefghijk") {
		t.Fatal("regression")
	}

}
func TestAudit20260906CustomDefaultTransportIgnoresProxy(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	called := false
	http.DefaultTransport = auditRoundTrip(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("direct")), Request: r}, nil
	})
	c := defaultHTTPClient("not-a-proxy", "", false)
	r, e := c.Get("https://example.test/")
	if r != nil {
		r.Body.Close()
	}
	if e == nil || called {
		t.Fatalf("regression: %v called=%v", e, called)
	}

}

type auditRoundTrip func(*http.Request) (*http.Response, error)

func (f auditRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAudit20260906AutoSubtitleFlagIgnored(t *testing.T) {
	c, srv := auditSubtitleClient(t)
	defer srv.Close()
	defer c.Close()
	r, e := c.WriteRequestedSubtitleSidecars(context.Background(), "abcdefghijk", &VideoInfo{ID: "abcdefghijk", Title: "title"}, SubtitleSidecarOptions{Languages: []string{"en"}, IncludeAuto: true, OutputFormat: SubtitleOutputFormatSRT, OutputTemplate: filepath.Join(t.TempDir(), "x.%(ext)s")})
	if e != nil || len(r.Outcomes) != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	b, _ := os.ReadFile(r.Outcomes[0].Path)
	if !strings.Contains(string(b), "automatic") {
		t.Fatalf("regression: %s", b)
	}

}
func TestAudit20260906ExcludedSubtitlesReappear(t *testing.T) {
	c, srv := auditSubtitleClient(t)
	defer srv.Close()
	defer c.Close()
	r, e := c.WriteRequestedSubtitleSidecars(context.Background(), "abcdefghijk", &VideoInfo{ID: "abcdefghijk", Title: "title"}, SubtitleSidecarOptions{Languages: []string{"all", "-en"}, RequestAll: true, IncludeManual: true, OutputFormat: SubtitleOutputFormatSRT, OutputTemplate: filepath.Join(t.TempDir(), "x.%(ext)s")})
	if e != nil || r.WrittenCount() != 0 {
		t.Fatalf("regression: %+v %v", r, e)
	}

}
func TestAudit20260906NilTranscriptDestroysFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.srt")
	os.WriteFile(p, []byte("old"), 0600)
	var pan any
	func() { defer func() { pan = recover() }(); WriteTranscript(p, nil, SubtitleOutputFormatSRT) }()
	b, _ := os.ReadFile(p)
	if pan != nil || string(b) != "old" {
		t.Fatalf("regression: %v %q", pan, b)
	}

}
func TestAudit20260906PathDirBreaksStdout(t *testing.T) {
	got := EffectiveOutputTemplate(OutputTemplateOptions{OutputTemplate: "-", OutputPathDir: t.TempDir()})
	if got != "-" {
		t.Fatal("regression")
	}

}
func TestAudit20260906ReservedMultiDot(t *testing.T) {
	got := sanitizeOutputToken("CON.foo.bar")
	if got == "CON.foo.bar" {
		t.Fatal("regression", got)
	}

}
func TestAudit20260906FinalItemCancellationLost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, f := RunPlaylistItems(ctx, []PlaylistItem{{VideoID: "abcdefghijk"}}, PlaylistRunOptions{}, func(context.Context, PlaylistItem, int) error { cancel(); return context.Canceled })
	if !s.Aborted || !errors.Is(s.AbortErr, context.Canceled) || len(f) != 1 || !errors.Is(f[0].Err, context.Canceled) {
		t.Fatalf("regression: %+v %+v", s, f)
	}

}
func TestAudit20260906StdoutByteCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "MEDIA") }))
	defer srv.Close()
	c := New(Config{HTTPClient: srv.Client()})
	defer c.Close()
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "stub:", media: &source.Media{ID: "42", Formats: []FormatInfo{{Itag: 1, URL: srv.URL, HasAudio: true, HasVideo: true}}}}}
	p := filepath.Join(t.TempDir(), "stdout")
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	old := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = old; f.Close() }()
	r, e := c.Download(context.Background(), "stub:42", DownloadOptions{Itag: 1, OutputPath: "-"})
	f.Sync()
	b, _ := os.ReadFile(p)
	if e != nil || r.Bytes != 5 || string(b) != "MEDIA" {
		t.Fatalf("regression: %+v %v %s", r, e, b)
	}

}
func TestAudit20260906FilenameRenderedTwice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "media") }))
	defer srv.Close()
	c := New(Config{HTTPClient: srv.Client(), DownloadTransport: DownloadTransportConfig{MaxConcurrency: 1}})
	defer c.Close()
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "stub:", media: &source.Media{ID: "42", Title: "literal %(id)s", Formats: []FormatInfo{{Itag: 1, URL: srv.URL, MimeType: "video/mp4", HasAudio: true, HasVideo: true}}}}}
	info, e := c.GetVideo(context.Background(), "stub:42")
	if e != nil {
		t.Fatal(e)
	}
	predicted, e := PredictOutputFilename(info, info.Formats, OutputFilenameOptions{OutputTemplate: filepath.Join(t.TempDir(), "%(title)s.%(ext)s")})
	if e != nil {
		t.Fatal(e)
	}
	r, e := c.Download(context.Background(), "stub:42", DownloadOptions{OutputPath: predicted, OutputPathResolved: true, Itag: 1})
	if e != nil || r.OutputPath != predicted {
		t.Fatalf("regression: %s %+v %v", predicted, r, e)
	}

}
func auditSubtitleClient(t *testing.T) (*Client, *httptest.Server) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<transcript><text start=\"0\" dur=\"1\">%s</text></transcript>", strings.TrimPrefix(r.URL.Path, "/"))
	}))
	c := New(Config{HTTPClient: srv.Client()})
	var response innertube.PlayerResponse
	raw := fmt.Sprintf(`{"captions":{"playerCaptionsTracklistRenderer":{"captionTracks":[{"languageCode":"en","baseUrl":%q},{"languageCode":"en","kind":"asr","baseUrl":%q}]}}}`, srv.URL+"/manual", srv.URL+"/automatic")
	if e := json.Unmarshal([]byte(raw), &response); e != nil {
		t.Fatal(e)
	}
	c.putSession("abcdefghijk", videoSession{Response: &response, Info: &VideoInfo{ID: "abcdefghijk"}})
	return c, srv
}
