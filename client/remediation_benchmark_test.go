package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkAuditDirectThousandChunks(b *testing.B) {
	client := &http.Client{Transport: auditRoundTrip(func(r *http.Request) (*http.Response, error) {
		var start, end int64
		fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		return &http.Response{StatusCode: 206, Header: http.Header{"Content-Range": {fmt.Sprintf("bytes %d-%d/16000", start, end)}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", int(end-start+1)))), ContentLength: end - start + 1, Request: r}, nil
	})}
	path := filepath.Join(b.TempDir(), "out")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := downloadURLChunked(context.Background(), client, "https://media.test/file", path, effectiveDownloadTransportConfig{ChunkSize: 16, MaxConcurrency: 4}, "", nil, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAuditLoadDecipherer(b *testing.B) {
	c := New(Config{HTTPClient: &http.Client{Transport: auditRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("var player={};")), Request: r}, nil
	})}})
	defer c.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.loadDecipherer(context.Background(), "https://www.youtube.com/s/player/abcd/base.js"); err != nil {
			b.Fatal(err)
		}
	}
}
