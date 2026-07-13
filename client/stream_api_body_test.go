package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type streamingBody struct {
	chunks [][]byte
	idx    int
}

func (s *streamingBody) Read(p []byte) (int, error) {
	if s.idx >= len(s.chunks) {
		return 0, io.EOF
	}
	n := copy(p, s.chunks[s.idx])
	s.idx++
	return n, nil
}

// ctxAwareStreamingBody emits chunks with a delay and mirrors net/http's real
// behavior of aborting an in-flight body read when the request context is
// canceled or its deadline expires. Mock transports normally bypass this
// (their body ignores the context), so this type lets a mock-based test
// reproduce the production failure mode where a deadline-bound media GET
// request yields context.DeadlineExceeded mid-read.
type ctxAwareStreamingBody struct {
	chunks [][]byte
	idx    int
	delay  time.Duration
	ctx    context.Context
}

func (s *ctxAwareStreamingBody) Read(p []byte) (int, error) {
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return 0, s.ctx.Err()
		default:
		}
	}
	if s.idx >= len(s.chunks) {
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	if s.ctx != nil {
		select {
		case <-s.ctx.Done():
			return 0, s.ctx.Err()
		default:
		}
	}
	n := copy(p, s.chunks[s.idx])
	s.idx++
	return n, nil
}

const playerJSONStreamBody = `{"playabilityStatus":{"status":"OK"},"videoDetails":{"videoId":"jNQXAC9IVRw","title":"Me at the zoo","author":"jawed"},"streamingData":{"formats":[{"itag":18,"url":"https://stream.local/v18.mp4","mimeType":"video/mp4","bitrate":1000}]}}`

func TestOpenStream_BodyReadableAfterReturn(t *testing.T) {
	want := "stream-body-survives-return"
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(playerJSONStreamBody)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/base.js"></script></html>`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Host == "stream.local":
				body := &streamingBody{chunks: [][]byte{[]byte(want)}}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(body),
				}, nil
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				return nil, nil
			}
		}),
	}
	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})

	rc, _, err := c.OpenFormatStream(context.Background(), "jNQXAC9IVRw", 18)
	if err != nil {
		t.Fatalf("OpenFormatStream() error = %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream after OpenStream returned: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body mismatch: got %q want %q", string(got), want)
	}
}

// TestStreamBody_ReadAfterCloseReturnsEOF asserts that a Read after Close
// returns io.EOF rather than racing into the underlying body. The previous
// mutex-based implementation unlocked before calling rc.Read, leaving a
// window where Close could run rc.Close while a Read was in flight.
func TestStreamBody_ReadAfterCloseReturnsEOF(t *testing.T) {
	body := &streamingBody{chunks: [][]byte{[]byte("payload")}}
	sb := &streamBody{rc: io.NopCloser(body), cancel: func() {}}

	buf := make([]byte, 4)
	n, err := sb.Read(buf)
	if err != nil || n != 4 {
		t.Fatalf("first Read: n=%d err=%v", n, err)
	}

	if err := sb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	n, err = sb.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("Read after Close: n=%d err=%v, want io.EOF", n, err)
	}
	if n != 0 {
		t.Fatalf("Read after Close returned %d bytes, want 0", n)
	}

	if err := sb.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestStreamBody_ConcurrentReadClose exercises the Close/Read race without
// a real network body. The atomic-flag implementation guarantees no Read
// proceeds after Close sets the flag, and Close is idempotent under sync.Once.
func TestStreamBody_ConcurrentReadClose(t *testing.T) {
	for i := 0; i < 200; i++ {
		body := &streamingBody{chunks: [][]byte{[]byte("x")}}
		sb := &streamBody{rc: io.NopCloser(body), cancel: func() {}}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = sb.Read(make([]byte, 1))
		}()
		go func() {
			defer wg.Done()
			_ = sb.Close()
		}()
		wg.Wait()

		n, err := sb.Read(make([]byte, 1))
		if !errors.Is(err, io.EOF) || n != 0 {
			t.Fatalf("iteration %d: post-close Read n=%d err=%v, want EOF", i, n, err)
		}
	}
}

func TestOpenStream_BodyCloseReleasesContext(t *testing.T) {
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(playerJSONStreamBody)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/base.js"></script></html>`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Host == "stream.local":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("ok")),
				}, nil
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				return nil, nil
			}
		}),
	}
	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
	})

	rc, _, err := c.OpenFormatStream(context.Background(), "jNQXAC9IVRw", 18)
	if err != nil {
		t.Fatalf("OpenFormatStream() error = %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("first Close() = %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("second Close() = %v", err)
	}
}

// TestOpenStream_BodyReadablePastRequestTimeout asserts that the media stream
// body remains readable after the package RequestTimeout deadline elapses.
// Before the fix, OpenStream bound the media GET request to the same
// timeout-bearing context used for metadata requests, so net/http aborted the
// in-flight body read at the deadline and surfaced context.DeadlineExceeded to
// the caller. The fix issues the media request with a deadline-free, cancel-only
// context derived from the caller's ctx.
func TestOpenStream_BodyReadablePastRequestTimeout(t *testing.T) {
	want := "slow-stream-survives-request-timeout-deadline"
	// RequestTimeout is deliberately shorter than the per-chunk read delay so
	// the deadline elapses while the body is still being read.
	requestTimeout := 60 * time.Millisecond
	perChunkDelay := 40 * time.Millisecond
	chunks := [][]byte{
		[]byte("slow-"),
		[]byte("stream-"),
		[]byte("survives-"),
		[]byte("request-timeout-deadline"),
	}
	httpClient := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/youtubei/v1/player"):
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(playerJSONStreamBody)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Path == "/watch":
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(bytes.NewBufferString(`<html><script src="/s/player/test/base.js"></script></html>`)),
				}, nil
			case r.Method == http.MethodGet && r.URL.Host == "stream.local":
				// ctxAwareStreamingBody mirrors net/http's real behavior of
				// aborting the body read when the request context deadline
				// expires, so the test reproduces the production failure
				// mode without a real network round-trip.
				body := &ctxAwareStreamingBody{chunks: chunks, delay: perChunkDelay, ctx: r.Context()}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(body),
				}, nil
			default:
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				return nil, nil
			}
		}),
	}
	c := New(Config{
		HTTPClient:      httpClient,
		ClientOverrides: []string{"mweb"},
		RequestTimeout:  requestTimeout,
	})

	rc, _, err := c.OpenFormatStream(context.Background(), "jNQXAC9IVRw", 18)
	if err != nil {
		t.Fatalf("OpenFormatStream() error = %v", err)
	}
	defer rc.Close()

	// Sleep past the RequestTimeout deadline before reading so the test
	// explicitly exercises the post-deadline read path rather than racing it.
	time.Sleep(requestTimeout + 50*time.Millisecond)

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stream past RequestTimeout deadline: %v", err)
	}
	if string(got) != want {
		t.Fatalf("body mismatch: got %q want %q", string(got), want)
	}
}
