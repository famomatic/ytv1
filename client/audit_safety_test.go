package client

import (
	"context"
	"fmt"
	"github.com/famomatic/ytv1/internal/source"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestChunkFailureResumePreservesOnlyCompletePrefix(t *testing.T) {
	var retry atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Range") {
		case "bytes=0-0":
			w.Header().Set("Content-Range", "bytes 0-0/8")
			w.WriteHeader(206)
			fmt.Fprint(w, "A")
		case "bytes=0-3":
			if !retry.Load() {
				w.WriteHeader(500)
				return
			}
			w.Header().Set("Content-Range", "bytes 0-3/8")
			w.WriteHeader(206)
			fmt.Fprint(w, "ABCD")
		case "bytes=4-7":
			w.Header().Set("Content-Range", "bytes 4-7/8")
			w.WriteHeader(206)
			fmt.Fprint(w, "EFGH")
		default:
			t.Errorf("unexpected range %q", r.Header.Get("Range"))
			w.WriteHeader(416)
		}
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "out.bin")
	cfg := DownloadTransportConfig{EnableChunked: true, ChunkSize: 4, MaxConcurrency: 2}
	_, err := downloadURLToPathWithHeadersAndPart(context.Background(), srv.Client(), srv.URL, path, true, true, cfg, "", nil)
	if err == nil {
		t.Fatal("expected failed first attempt")
	}
	st, err := os.Stat(path + ".part")
	if err != nil || st.Size() != 0 {
		t.Fatalf("partial file must retain no unverified prefix: %v %v", st, err)
	}
	retry.Store(true)
	n, err := downloadURLToPathWithHeadersAndPart(context.Background(), srv.Client(), srv.URL, path, true, true, cfg, "", nil)
	data, _ := os.ReadFile(path)
	if err != nil || n != 8 || string(data) != "ABCDEFGH" {
		t.Fatalf("n=%d err=%v data=%q", n, err, data)
	}
}

func TestArchiveIndependentHandlesAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive")
	a, err := OpenDownloadArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := OpenDownloadArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = a.Add("jNQXAC9IVRw"); err != nil {
		t.Fatal(err)
	}
	if err = b.Add("dQw4w9WgXcQ"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "jNQXAC9IVRw\n") || !strings.Contains(string(data), "dQw4w9WgXcQ\n") {
		t.Fatalf("lost archive entry: %q", data)
	}
}

func TestSourceStreamCloseCancelsPendingRequest(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(cancelled) }))
	defer srv.Close()
	c := New(Config{HTTPClient: srv.Client()})
	c.sources = []source.Source{&stubSource{name: "stub", prefix: "https://stub.test/", media: stubMedia(srv)}}
	rc, _, err := c.OpenStream(context.Background(), "https://stub.test/v/42", StreamOptions{})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	rc.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel media request")
	}
}

func TestClonedPartsAreIndependent(t *testing.T) {
	info := &VideoInfo{Formats: []FormatInfo{{Parts: []string{"original"}}}}
	clone := cloneVideoInfo(info)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			clone.Formats[0].Parts[0] = "modified"
		}
	}()
	for i := 0; i < 10000; i++ {
		if info.Formats[0].Parts[0] != "original" {
			t.Error("cache alias")
		}
	}
	<-done
}
