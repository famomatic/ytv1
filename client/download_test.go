package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/types"
)

func TestDownloadURLToWriter_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	n, err := downloadURLToWriter(context.Background(), srv.Client(), srv.URL, &buf)
	if err != nil {
		t.Fatalf("downloadURLToWriter() error = %v", err)
	}
	if n != int64(len("payload")) {
		t.Fatalf("downloadURLToWriter() bytes = %d, want %d", n, len("payload"))
	}
	if got := buf.String(); got != "payload" {
		t.Fatalf("downloadURLToWriter() body = %q, want %q", got, "payload")
	}
}

func TestDownloadURLToWriter_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if _, err := downloadURLToWriter(context.Background(), srv.Client(), srv.URL, &buf); err == nil {
		t.Fatalf("downloadURLToWriter() error = nil, want non-nil")
	}
}

func TestDownloadStream_UsesSourceClientMediaHeaders(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		if !strings.Contains(gotUA, "youtube.vr.oculus") {
			http.Error(w, "wrong user agent", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("payload"))
	}))
	defer srv.Close()

	c := &Client{config: Config{
		HTTPClient:        srv.Client(),
		DownloadTransport: DownloadTransportConfig{EnableChunked: false},
	}}
	outputPath := filepath.Join(t.TempDir(), "out.bin")
	err := c.downloadStream(context.Background(), "video-id", srv.URL, outputPath, FormatInfo{
		Itag:         315,
		URL:          srv.URL,
		Protocol:     "https",
		SourceClient: "android_vr",
	}, false, false)
	if err != nil {
		t.Fatalf("downloadStream() error = %v, ua=%q", err, gotUA)
	}
	body, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "payload" {
		t.Fatalf("downloaded body=%q, want payload", got)
	}
}

func TestShouldRetryDefaultWithFallbackSingleOnDownload403(t *testing.T) {
	err := wrapDownloadFailure(&downloadHTTPStatusError{StatusCode: http.StatusForbidden}, AttemptDetail{
		HTTPStatus: http.StatusForbidden,
	})
	if !shouldRetryDefaultWithFallbackSingle(err, DownloadOptions{}) {
		t.Fatalf("expected default download 403 to retry fallback single")
	}
	if shouldRetryDefaultWithFallbackSingle(err, DownloadOptions{Itag: 18}) {
		t.Fatalf("explicit itag should not retry fallback single")
	}
	if shouldRetryDefaultWithFallbackSingle(err, DownloadOptions{FormatSelector: "315+251"}) {
		t.Fatalf("explicit selector should not retry fallback single")
	}
}

func TestNormalizeDownloadTransportConfig_DoesNotEnableChunkedByDefault(t *testing.T) {
	cfg := normalizeDownloadTransportConfig(DownloadTransportConfig{})
	if cfg.EnableChunked {
		t.Fatalf("EnableChunked=true, want false by default")
	}
	if cfg.ChunkSize <= 0 {
		t.Fatalf("ChunkSize=%d, want default chunk size retained for explicit chunked use", cfg.ChunkSize)
	}
	if cfg.MaxConcurrency <= 0 {
		t.Fatalf("MaxConcurrency=%d, want default concurrency retained for explicit chunked use", cfg.MaxConcurrency)
	}
}

func TestDownloadURLToWriter_RetryOnTransientStatus(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok-after-retry"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	n, err := downloadURLToWriterWithConfig(context.Background(), srv.Client(), srv.URL, &buf, DownloadTransportConfig{
		MaxRetries:     2,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToWriterWithConfig() error = %v", err)
	}
	if n != int64(len("ok-after-retry")) {
		t.Fatalf("downloadURLToWriterWithConfig() bytes = %d, want %d", n, len("ok-after-retry"))
	}
	if got := buf.String(); got != "ok-after-retry" {
		t.Fatalf("downloadURLToWriterWithConfig() body = %q, want %q", got, "ok-after-retry")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestDownloadURLToWriter_RetryOnThrottledRate(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			flusher, _ := w.(http.Flusher)
			for i := 0; i < 8; i++ {
				_, _ = w.Write([]byte("x"))
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(15 * time.Millisecond)
			}
			return
		}
		_, _ = w.Write([]byte("fast-after-throttle"))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	n, err := downloadURLToWriterWithConfig(context.Background(), srv.Client(), srv.URL, &buf, DownloadTransportConfig{
		MaxRetries:                  1,
		InitialBackoff:              time.Millisecond,
		MaxBackoff:                  time.Millisecond,
		ThrottledRateBytesPerSecond: 1024,
		ThrottledRateMinDuration:    20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToWriterWithConfig() error = %v", err)
	}
	if n != int64(len("fast-after-throttle")) {
		t.Fatalf("downloadURLToWriterWithConfig() bytes = %d, want %d", n, len("fast-after-throttle"))
	}
	if got := buf.String(); got != "fast-after-throttle" {
		t.Fatalf("buffer=%q, want retry body without partial throttled bytes", got)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
	}
}

type readSizeRecorder struct {
	lastLen int
	done    bool
}

func (r *readSizeRecorder) Read(p []byte) (int, error) {
	r.lastLen = len(p)
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = 'x'
	return 1, nil
}

func TestRateLimitedReaderCapsChunkSize(t *testing.T) {
	src := &readSizeRecorder{}
	r := rateLimitedReader(context.Background(), src, 20)
	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("Read() n=%d, want 1", n)
	}
	if src.lastLen != 2 {
		t.Fatalf("underlying read len=%d, want 2", src.lastLen)
	}
}

func TestDownloadURLToPath_ResumeAppend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=3-" {
			t.Fatalf("range header=%q, want %q", got, "bytes=3-")
		}
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "def")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "resume.bin")
	if err := os.WriteFile(out, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, true, DownloadTransportConfig{
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != 6 {
		t.Fatalf("downloadURLToPath() bytes=%d, want 6", n)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "abcdef" {
		t.Fatalf("final content=%q, want %q", got, "abcdef")
	}
}

func TestDownloadURLToPath_ResumeAppendRetryTruncatesPartialAttempt(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=3-" {
			t.Fatalf("range header=%q, want %q", got, "bytes=3-")
		}
		w.Header().Set("Content-Range", "bytes 3-10/11")
		w.WriteHeader(http.StatusPartialContent)
		if atomic.AddInt32(&calls, 1) == 1 {
			flusher, _ := w.(http.Flusher)
			for i := 0; i < 8; i++ {
				_, _ = w.Write([]byte("x"))
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(15 * time.Millisecond)
			}
			return
		}
		_, _ = io.WriteString(w, "defghijk")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "resume-throttle.bin")
	if err := os.WriteFile(out, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, true, DownloadTransportConfig{
		MaxRetries:                  1,
		InitialBackoff:              time.Millisecond,
		MaxBackoff:                  time.Millisecond,
		ThrottledRateBytesPerSecond: 1024,
		ThrottledRateMinDuration:    20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != int64(len("abcdefghijk")) {
		t.Fatalf("downloadURLToPath() bytes=%d, want %d", n, len("abcdefghijk"))
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "abcdefghijk" {
		t.Fatalf("final content=%q, want abcdefghijk", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("call count=%d, want 2", got)
	}
}

func TestDownloadURLToPath_ResumeFallbackToFull(t *testing.T) {
	var sawRange bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("Range")) != "" {
			sawRange = true
			_, _ = io.WriteString(w, "full-data")
			return
		}
		_, _ = io.WriteString(w, "full-data")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "resume-fallback.bin")
	if err := os.WriteFile(out, []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, true, DownloadTransportConfig{
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if !sawRange {
		t.Fatal("expected initial resume range attempt")
	}
	if n != int64(len("full-data")) {
		t.Fatalf("downloadURLToPath() bytes=%d, want %d", n, len("full-data"))
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "full-data" {
		t.Fatalf("final content=%q, want %q", got, "full-data")
	}
}

func TestDownloadURLToPath_FullRewriteRetryTruncatesPartialAttempt(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := strings.TrimSpace(r.Header.Get("Range")); got != "bytes=0-" {
			t.Fatalf("range header=%q, want bytes=0-", got)
		}
		w.WriteHeader(http.StatusPartialContent)
		if atomic.AddInt32(&calls, 1) == 1 {
			flusher, _ := w.(http.Flusher)
			for i := 0; i < 8; i++ {
				_, _ = w.Write([]byte("x"))
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(15 * time.Millisecond)
			}
			return
		}
		_, _ = io.WriteString(w, "complete-after-retry")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "full-rewrite-throttle.bin")
	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		EnableChunked:               false,
		MaxRetries:                  1,
		InitialBackoff:              time.Millisecond,
		MaxBackoff:                  time.Millisecond,
		ThrottledRateBytesPerSecond: 1024,
		ThrottledRateMinDuration:    20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != int64(len("complete-after-retry")) {
		t.Fatalf("downloadURLToPath() bytes=%d, want %d", n, len("complete-after-retry"))
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(body); got != "complete-after-retry" {
		t.Fatalf("final content=%q, want complete-after-retry", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("call count=%d, want 2", got)
	}
}

func TestDownloadURLToPath_PartFileRenamesOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "complete")
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "video.bin")
	n, err := downloadURLToPathWithHeadersAndPart(context.Background(), srv.Client(), srv.URL, out, false, true, DownloadTransportConfig{}, "", nil)
	if err != nil {
		t.Fatalf("downloadURLToPathWithHeadersAndPart() error = %v", err)
	}
	if n != int64(len("complete")) {
		t.Fatalf("bytes=%d, want %d", n, len("complete"))
	}
	if _, err := os.Stat(out + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file should be renamed away, stat err=%v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "complete" {
		t.Fatalf("body=%q, want complete", string(body))
	}
}

func TestDownloadURLToPath_PartFileRenameRetriesTransientFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "complete-after-rename-retry")
	}))
	defer srv.Close()

	previousRenameFile := renameFile
	var renameCalls int32
	renameFile = func(oldpath, newpath string) error {
		if atomic.AddInt32(&renameCalls, 1) == 1 {
			return &os.PathError{Op: "rename", Path: oldpath, Err: os.ErrPermission}
		}
		return previousRenameFile(oldpath, newpath)
	}
	defer func() {
		renameFile = previousRenameFile
	}()

	dir := t.TempDir()
	out := filepath.Join(dir, "video.bin")
	n, err := downloadURLToPathWithHeadersAndPart(context.Background(), srv.Client(), srv.URL, out, false, true, DownloadTransportConfig{
		FileAccessRetries: 1,
		FileAccessBackoff: time.Millisecond,
	}, "", nil)
	if err != nil {
		t.Fatalf("downloadURLToPathWithHeadersAndPart() error = %v", err)
	}
	if n != int64(len("complete-after-rename-retry")) {
		t.Fatalf("bytes=%d, want %d", n, len("complete-after-rename-retry"))
	}
	if got := atomic.LoadInt32(&renameCalls); got != 2 {
		t.Fatalf("rename calls=%d, want 2", got)
	}
	if _, err := os.Stat(out + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file should be renamed away, stat err=%v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "complete-after-rename-retry" {
		t.Fatalf("body=%q, want complete-after-rename-retry", string(body))
	}
}

func TestDownloadURLToPath_FileCreateRetriesTransientFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "complete-after-create-retry")
	}))
	defer srv.Close()

	previousCreateFile := createFile
	var createCalls int32
	createFile = func(name string) (*os.File, error) {
		if atomic.AddInt32(&createCalls, 1) == 1 {
			return nil, &os.PathError{Op: "create", Path: name, Err: os.ErrPermission}
		}
		return previousCreateFile(name)
	}
	defer func() {
		createFile = previousCreateFile
	}()

	dir := t.TempDir()
	out := filepath.Join(dir, "video.bin")
	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		FileAccessRetries: 1,
		FileAccessBackoff: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != int64(len("complete-after-create-retry")) {
		t.Fatalf("bytes=%d, want %d", n, len("complete-after-create-retry"))
	}
	if got := atomic.LoadInt32(&createCalls); got != 2 {
		t.Fatalf("create calls=%d, want 2", got)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "complete-after-create-retry" {
		t.Fatalf("body=%q, want complete-after-create-retry", string(body))
	}
}

func TestDownloadURLToPath_FileAccessRetriesDefaultToYTDLPValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "complete-after-default-retries")
	}))
	defer srv.Close()

	previousCreateFile := createFile
	var createCalls int32
	createFile = func(name string) (*os.File, error) {
		if atomic.AddInt32(&createCalls, 1) <= 3 {
			return nil, &os.PathError{Op: "create", Path: name, Err: os.ErrPermission}
		}
		return previousCreateFile(name)
	}
	defer func() {
		createFile = previousCreateFile
	}()

	dir := t.TempDir()
	out := filepath.Join(dir, "video.bin")
	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		FileAccessBackoff: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != int64(len("complete-after-default-retries")) {
		t.Fatalf("bytes=%d, want %d", n, len("complete-after-default-retries"))
	}
	if got := atomic.LoadInt32(&createCalls); got != 4 {
		t.Fatalf("create calls=%d, want 4", got)
	}
}

func TestDownloadURLToPath_FileAccessRetriesNegativeDisablesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "unused")
	}))
	defer srv.Close()

	previousCreateFile := createFile
	var createCalls int32
	createFile = func(name string) (*os.File, error) {
		atomic.AddInt32(&createCalls, 1)
		return nil, &os.PathError{Op: "create", Path: name, Err: os.ErrPermission}
	}
	defer func() {
		createFile = previousCreateFile
	}()

	dir := t.TempDir()
	out := filepath.Join(dir, "video.bin")
	_, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		FileAccessRetries: -1,
		FileAccessBackoff: time.Millisecond,
	})
	if err == nil {
		t.Fatalf("downloadURLToPath() error = nil, want create error")
	}
	if got := atomic.LoadInt32(&createCalls); got != 1 {
		t.Fatalf("create calls=%d, want 1", got)
	}
}

func TestCleanupIntermediateFile_RemoveRetriesTransientFailure(t *testing.T) {
	previousRemoveFile := removeFile
	var removeCalls int32
	removeFile = func(name string) error {
		if atomic.AddInt32(&removeCalls, 1) == 1 {
			return &os.PathError{Op: "remove", Path: name, Err: os.ErrPermission}
		}
		return previousRemoveFile(name)
	}
	defer func() {
		removeFile = previousRemoveFile
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, "intermediate.bin")
	if err := os.WriteFile(path, []byte("temp"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	c := New(Config{
		DownloadTransport: DownloadTransportConfig{
			FileAccessRetries: 1,
			FileAccessBackoff: time.Millisecond,
		},
	})
	c.cleanupIntermediateFile("video-id", path, false)

	if got := atomic.LoadInt32(&removeCalls); got != 2 {
		t.Fatalf("remove calls=%d, want 2", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("intermediate file should be removed, stat err=%v", err)
	}
}

func TestDownloadHLS_PartFileRenameRetriesTransientFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1.0,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/segment.ts":
			_, _ = io.WriteString(w, "hls-segment")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	previousRenameFile := renameFile
	var renameCalls int32
	renameFile = func(oldpath, newpath string) error {
		if atomic.AddInt32(&renameCalls, 1) == 1 {
			return &os.PathError{Op: "rename", Path: oldpath, Err: os.ErrPermission}
		}
		return previousRenameFile(oldpath, newpath)
	}
	defer func() {
		renameFile = previousRenameFile
	}()

	dir := t.TempDir()
	out := filepath.Join(dir, "hls.ts")
	c := New(Config{
		HTTPClient: srv.Client(),
		DownloadTransport: DownloadTransportConfig{
			FileAccessRetries: 1,
			FileAccessBackoff: time.Millisecond,
		},
	})
	result, err := c.downloadHLS(context.Background(), "video-id", srv.URL+"/playlist.m3u8", out, FormatInfo{Itag: 96}, true)
	if err != nil {
		t.Fatalf("downloadHLS() error = %v", err)
	}
	if got := atomic.LoadInt32(&renameCalls); got != 2 {
		t.Fatalf("rename calls=%d, want 2", got)
	}
	if result.Bytes != int64(len("hls-segment")) {
		t.Fatalf("result bytes=%d, want %d", result.Bytes, len("hls-segment"))
	}
	if _, err := os.Stat(out + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file should be renamed away, stat err=%v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "hls-segment" {
		t.Fatalf("body=%q, want hls-segment", string(body))
	}
}

func TestDownloadDASH_PartFileRenameRetriesTransientFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.mpd":
			w.Header().Set("Content-Type", "application/dash+xml")
			_, _ = io.WriteString(w, `<?xml version="1.0"?>
<MPD type="static" xmlns="urn:mpeg:dash:schema:mpd:2011">
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="248" bandwidth="1000000">
        <SegmentTemplate timescale="1" media="seg-$Number$.m4s" startNumber="1">
          <SegmentTimeline>
            <S d="1" r="0"/>
          </SegmentTimeline>
        </SegmentTemplate>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`)
		case "/seg-1.m4s":
			_, _ = io.WriteString(w, "dash-segment")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	previousRenameFile := renameFile
	var renameCalls int32
	renameFile = func(oldpath, newpath string) error {
		if atomic.AddInt32(&renameCalls, 1) == 1 {
			return &os.PathError{Op: "rename", Path: oldpath, Err: os.ErrPermission}
		}
		return previousRenameFile(oldpath, newpath)
	}
	defer func() {
		renameFile = previousRenameFile
	}()

	dir := t.TempDir()
	out := filepath.Join(dir, "dash.m4s")
	c := New(Config{
		HTTPClient: srv.Client(),
		DownloadTransport: DownloadTransportConfig{
			FileAccessRetries: 1,
			FileAccessBackoff: time.Millisecond,
		},
	})
	result, err := c.downloadDASH(context.Background(), "video-id", srv.URL+"/manifest.mpd", out, FormatInfo{Itag: 248}, true)
	if err != nil {
		t.Fatalf("downloadDASH() error = %v", err)
	}
	if got := atomic.LoadInt32(&renameCalls); got != 2 {
		t.Fatalf("rename calls=%d, want 2", got)
	}
	if result.Bytes != int64(len("dash-segment")) {
		t.Fatalf("result bytes=%d, want %d", result.Bytes, len("dash-segment"))
	}
	if _, err := os.Stat(out + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part file should be renamed away, stat err=%v", err)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "dash-segment" {
		t.Fatalf("body=%q, want dash-segment", string(body))
	}
}

func TestDownloadURLToPath_PartFileResume(t *testing.T) {
	var sawRange bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=3-" {
			t.Fatalf("range header=%q, want bytes=3-", got)
		}
		sawRange = true
		w.Header().Set("Content-Range", "bytes 3-5/6")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "def")
	}))
	defer srv.Close()

	dir := t.TempDir()
	out := filepath.Join(dir, "resume.bin")
	if err := os.WriteFile(out+".part", []byte("abc"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	n, err := downloadURLToPathWithHeadersAndPart(context.Background(), srv.Client(), srv.URL, out, true, true, DownloadTransportConfig{}, "", nil)
	if err != nil {
		t.Fatalf("downloadURLToPathWithHeadersAndPart() error = %v", err)
	}
	if !sawRange {
		t.Fatalf("expected resume range request")
	}
	if n != 6 {
		t.Fatalf("bytes=%d, want 6", n)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(body) != "abcdef" {
		t.Fatalf("body=%q, want abcdef", string(body))
	}
}

func TestDownloadURLToPath_Chunked(t *testing.T) {
	payload := []byte(strings.Repeat("chunk-data-", 512))
	var rangeCalls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		if start < 0 || end < start || end >= len(payload) {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		atomic.AddInt32(&rangeCalls, 1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "chunked.bin")
	n, err := downloadURLToPath(context.Background(), srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		EnableChunked:  true,
		ChunkSize:      1024,
		MaxConcurrency: 4,
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
		MaxBackoff:     time.Millisecond,
	})
	if err != nil {
		t.Fatalf("downloadURLToPath() error = %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("downloadURLToPath() bytes=%d, want %d", n, len(payload))
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatal("chunked output mismatch")
	}
	if atomic.LoadInt32(&rangeCalls) <= 1 {
		t.Fatalf("expected multiple range calls, got %d", atomic.LoadInt32(&rangeCalls))
	}
}

func TestDownloadURLToPath_ChunkedCancel(t *testing.T) {
	payload := []byte(strings.Repeat("x", 1024*64))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		var start, end int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	out := filepath.Join(t.TempDir(), "chunked-cancel.bin")
	_, err := downloadURLToPath(ctx, srv.Client(), srv.URL, out, false, DownloadTransportConfig{
		EnableChunked:  true,
		ChunkSize:      1024,
		MaxConcurrency: 4,
		MaxRetries:     0,
	})
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation error, got %v", err)
	}
}

func TestDownloadURLToPathWithHeaders_AppliesMediaHeaders(t *testing.T) {
	var gotUA, gotReferer, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotReferer = r.Header.Get("Referer")
		gotOrigin = r.Header.Get("Origin")
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "headers.bin")
	_, err := downloadURLToPathWithHeaders(
		context.Background(),
		srv.Client(),
		srv.URL,
		out,
		false,
		DownloadTransportConfig{},
		"abc123",
		http.Header{"User-Agent": []string{"custom-agent/1.0"}},
	)
	if err != nil {
		t.Fatalf("downloadURLToPathWithHeaders() error = %v", err)
	}
	if gotUA != "custom-agent/1.0" {
		t.Fatalf("User-Agent=%q, want %q", gotUA, "custom-agent/1.0")
	}
	if gotReferer != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("Referer=%q", gotReferer)
	}
	if gotOrigin != "https://www.youtube.com" {
		t.Fatalf("Origin=%q", gotOrigin)
	}
}

type testMuxer struct {
	metas *[]types.Metadata
}

func (testMuxer) Available() bool { return true }

func (m testMuxer) Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error {
	if m.metas != nil {
		*m.metas = append(*m.metas, meta)
	}
	v, err := os.ReadFile(videoPath)
	if err != nil {
		return err
	}
	a, err := os.ReadFile(audioPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, append(v, a...), 0o644)
}

func TestDownloadRequiresMuxerForSeparateBestVideoAudio(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{
						"formats":[{"itag":18,"url":"https://media.example/low.mp4","mimeType":"video/mp4","bitrate":120000,"width":256,"height":144}],
						"adaptiveFormats":[
							{"itag":315,"url":"https://media.example/4k.webm","mimeType":"video/webm","bitrate":12000000,"width":3840,"height":2160},
							{"itag":251,"url":"https://media.example/audio.webm","mimeType":"audio/webm","bitrate":192000}
						]
					}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})
	_, err := c.Download(context.Background(), videoID, DownloadOptions{Mode: SelectionModeBest})
	if !errors.Is(err, ErrMuxerUnavailable) {
		t.Fatalf("Download() error = %v, want ErrMuxerUnavailable", err)
	}
}

func TestDownloadAndMerge_DefaultCleansIntermediateFiles(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	var events []DownloadEvent
	mediaBase := "https://media.example"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"adaptiveFormats":[
						{"itag":248,"url":"` + mediaBase + `/v.webm","mimeType":"video/webm","bitrate":1000},
						{"itag":251,"url":"` + mediaBase + `/a.webm","mimeType":"audio/webm","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("video")), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/a.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("audio")), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
		OnDownloadEvent: func(evt DownloadEvent) { events = append(events, evt) },
	})
	out := filepath.Join(t.TempDir(), "merged.webm")
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:       SelectionModeBest,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.OutputPath != out {
		t.Fatalf("output path=%q want=%q", res.OutputPath, out)
	}
	videoPath := out + ".f248.video"
	audioPath := out + ".f251.audio"
	if _, err := os.Stat(videoPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected video intermediate deleted, stat err=%v", err)
	}
	if _, err := os.Stat(audioPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected audio intermediate deleted, stat err=%v", err)
	}
	var hasMergeComplete, hasCleanupDelete bool
	for _, evt := range events {
		if evt.Stage == "merge" && evt.Phase == "complete" {
			hasMergeComplete = true
		}
		if evt.Stage == "cleanup" && evt.Phase == "delete" {
			hasCleanupDelete = true
		}
	}
	if !hasMergeComplete || !hasCleanupDelete {
		t.Fatalf("expected merge complete and cleanup delete events, got=%v", events)
	}
}

func TestDownloadAndMerge_NoEmbedMetadataSuppressesMergeMetadata(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	mediaBase := "https://media.example"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y","shortDescription":"desc","publishDate":"2020-01-02"},
					"streamingData":{"adaptiveFormats":[
						{"itag":248,"url":"` + mediaBase + `/v.webm","mimeType":"video/webm","bitrate":1000},
						{"itag":251,"url":"` + mediaBase + `/a.webm","mimeType":"audio/webm","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("video")), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/a.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("audio")), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	var metas []types.Metadata
	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{metas: &metas},
	})
	_, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:            SelectionModeBest,
		OutputPath:      filepath.Join(t.TempDir(), "merged.webm"),
		NoEmbedMetadata: true,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("captured metadata count=%d, want 1", len(metas))
	}
	if metas[0] != (types.Metadata{}) {
		t.Fatalf("metadata=%+v, want empty", metas[0])
	}
}

func TestDownloadAndMerge_MergeOutputFormat(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	mediaBase := "https://media.example"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"adaptiveFormats":[
						{"itag":248,"url":"` + mediaBase + `/v.webm","mimeType":"video/webm","bitrate":1000},
						{"itag":251,"url":"` + mediaBase + `/a.webm","mimeType":"audio/webm","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("video")), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/a.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("audio")), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
	})
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:              SelectionModeBest,
		MergeOutputFormat: "mkv",
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.OutputPath != "x [jNQXAC9IVRw].mkv" {
		t.Fatalf("OutputPath=%q, want x [jNQXAC9IVRw].mkv", res.OutputPath)
	}
}

func TestDownloadAndMerge_KeepIntermediateFiles(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	var events []DownloadEvent
	mediaBase := "https://media.example"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"adaptiveFormats":[
						{"itag":248,"url":"` + mediaBase + `/v.webm","mimeType":"video/webm","bitrate":1000},
						{"itag":251,"url":"` + mediaBase + `/a.webm","mimeType":"audio/webm","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/v.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("video")), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.String() == mediaBase+"/a.webm":
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("audio")), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
		OnDownloadEvent: func(evt DownloadEvent) { events = append(events, evt) },
	})
	out := filepath.Join(t.TempDir(), "merged.webm")
	_, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:                  SelectionModeBest,
		OutputPath:            out,
		KeepIntermediateFiles: true,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	videoPath := out + ".f248.video"
	audioPath := out + ".f251.audio"
	if _, err := os.Stat(videoPath); err != nil {
		t.Fatalf("expected video intermediate kept, stat err=%v", err)
	}
	if _, err := os.Stat(audioPath); err != nil {
		t.Fatalf("expected audio intermediate kept, stat err=%v", err)
	}
	var hasCleanupSkip bool
	for _, evt := range events {
		if evt.Stage == "cleanup" && evt.Phase == "skip" {
			hasCleanupSkip = true
		}
	}
	if !hasCleanupSkip {
		t.Fatalf("expected cleanup skip event, got=%v", events)
	}
}

func TestDownloadFailureProvidesAttemptDetails(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	mediaURL := "https://media.example/v.webm?itag=18&pot=token&sig=xyz"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{"formats":[
						{"itag":18,"url":"` + mediaURL + `","mimeType":"video/mp4","bitrate":1000}
					]}
				}`
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/base.js"></script></html>`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/s/player/test/base.js":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(testPlayerJS())),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.String(), "https://media.example/v.webm?"):
				return &http.Response{
					StatusCode: http.StatusForbidden,
					Body:       io.NopCloser(strings.NewReader("forbidden")),
					Header:     make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})

	_, err := c.Download(context.Background(), videoID, DownloadOptions{
		Itag: 18,
	})
	if err == nil {
		t.Fatal("expected download failure error, got nil")
	}

	attempts, ok := AttemptDetails(err)
	if !ok || len(attempts) != 1 {
		t.Fatalf("AttemptDetails() ok=%v attempts=%v err=%v", ok, attempts, err)
	}
	a := attempts[0]
	if a.Stage != "download" || a.HTTPStatus != http.StatusForbidden {
		t.Fatalf("unexpected stage/status: %+v", a)
	}
	if a.Itag != 18 || a.Protocol != "https" {
		t.Fatalf("unexpected itag/protocol: %+v", a)
	}
	if a.URLHost != "media.example" || a.URLHasN || !a.URLHasPOT || !a.URLHasSignature {
		t.Fatalf("unexpected url policy details: %+v", a)
	}
	if a.Client == "" {
		t.Fatalf("expected source client in attempt details, got: %+v", a)
	}
}

func TestDownloadKeepsCipheredHighQualitySelection(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{
						"adaptiveFormats":[
							{"itag":248,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-video.webm&s=abc&sp=sig","mimeType":"video/webm","bitrate":2000000},
							{"itag":251,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-audio.webm&s=xyz&sp=sig","mimeType":"audio/webm","bitrate":192000},
							{"itag":135,"url":"https://media.example/plain-video.mp4","mimeType":"video/mp4","bitrate":700000},
							{"itag":140,"url":"https://media.example/plain-audio.m4a","mimeType":"audio/mp4","bitrate":128000}
						]
					}
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/player_ias.vflset/en_US/base.js"></script></html>`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/s/player/test/player_ias.vflset/en_US/base.js":
				// Intentionally broken JS: preserving the ciphered high-quality
				// selection should fail before falling back to lower plain URLs.
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`var broken = true;`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://media.example/plain-video.mp4":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("video")),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://media.example/plain-audio.m4a":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("audio")),
					Header:     make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
	})

	out := filepath.Join(t.TempDir(), "merged.mp4")
	_, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:       SelectionModeBest,
		OutputPath: out,
	})
	if !errors.Is(err, ErrChallengeNotSolved) {
		t.Fatalf("Download() error = %v, want ErrChallengeNotSolved", err)
	}
}

func TestDownloadFallsBackToSingleWhenMergeChallengeUnsolved(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				body := `{
					"playabilityStatus":{"status":"OK"},
					"videoDetails":{"videoId":"jNQXAC9IVRw","title":"x","author":"y"},
					"streamingData":{
						"formats":[{"itag":18,"url":"https://media.example/muxed.mp4","mimeType":"video/mp4","bitrate":120000}],
						"adaptiveFormats":[
							{"itag":248,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-video.webm&s=abc&sp=sig","mimeType":"video/webm","bitrate":2000000},
							{"itag":251,"signatureCipher":"url=https%3A%2F%2Fmedia.example%2Fcipher-audio.webm&s=xyz&sp=sig","mimeType":"audio/webm","bitrate":192000}
						]
					}
				}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`<html><script src="/s/player/test/player_ias.vflset/en_US/base.js"></script></html>`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/s/player/test/player_ias.vflset/en_US/base.js":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`var broken = true;`)),
					Header:     make(http.Header),
				}, nil
			case r.Method == http.MethodGet && r.URL.String() == "https://media.example/muxed.mp4":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("muxed")),
					Header:     make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("not found")),
					Header:     make(http.Header),
				}, nil
			}
		}),
	}

	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		Muxer:           testMuxer{},
	})
	out := filepath.Join(t.TempDir(), "fallback.mp4")
	res, err := c.Download(context.Background(), videoID, DownloadOptions{
		Mode:       SelectionModeBest,
		OutputPath: out,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res.Itag != 18 {
		t.Fatalf("expected fallback muxed itag=18, got %d", res.Itag)
	}
}
