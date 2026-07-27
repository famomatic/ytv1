package client

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

// releasableBlockingWriter blocks in Write until released, modelling a consumer
// (e.g. a closed player behind an undraining pipe) that has stopped reading.
type releasableBlockingWriter struct{ release chan struct{} }

func (b releasableBlockingWriter) Write(p []byte) (int, error) {
	<-b.release
	return len(p), nil
}

// slowReader returns each chunk after a delay, then EOF — a slow *source*.
type slowReader struct {
	chunks [][]byte
	delay  time.Duration
	i      int
}

func (r *slowReader) Read(p []byte) (int, error) {
	time.Sleep(r.delay)
	if r.i >= len(r.chunks) {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.i])
	r.i++
	return n, nil
}

func TestCopyWithWriteStallGuardNormal(t *testing.T) {
	var dst bytes.Buffer
	if err := copyWithWriteStallGuard(context.Background(), &dst, strings.NewReader("hello world"), 100*time.Millisecond); err != nil {
		t.Fatalf("normal copy: %v", err)
	}
	if dst.String() != "hello world" {
		t.Fatalf("got %q", dst.String())
	}
}

func TestCopyWithWriteStallGuardStalledWriteFails(t *testing.T) {
	w := releasableBlockingWriter{release: make(chan struct{})}
	defer close(w.release) // unblock the abandoned goroutine when the test ends
	start := time.Now()
	err := copyWithWriteStallGuard(context.Background(), w, strings.NewReader("data"), 60*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("expected stall error, got %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("guard fired too slowly: %v", d)
	}
}

// A slow source must NOT trip the guard: the guard flags a blocked write, not a
// slow read, even when the inter-read gap exceeds the stall window.
func TestCopyWithWriteStallGuardSlowSourceOK(t *testing.T) {
	src := &slowReader{chunks: [][]byte{[]byte("a"), []byte("b"), []byte("c")}, delay: 80 * time.Millisecond}
	var dst bytes.Buffer
	if err := copyWithWriteStallGuard(context.Background(), &dst, src, 40*time.Millisecond); err != nil {
		t.Fatalf("slow source should not trip guard: %v", err)
	}
	if dst.String() != "abc" {
		t.Fatalf("got %q", dst.String())
	}
}

func TestCopyWithWriteStallGuardDisabled(t *testing.T) {
	var dst bytes.Buffer
	if err := copyWithWriteStallGuard(context.Background(), &dst, strings.NewReader("xyz"), 0); err != nil {
		t.Fatalf("disabled guard: %v", err)
	}
	if dst.String() != "xyz" {
		t.Fatalf("got %q", dst.String())
	}
}
