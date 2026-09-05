package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

type auditRoundTripper func(*http.Request) (*http.Response, error)

func (f auditRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func BenchmarkAuditHLSThousandSegments(b *testing.B) {
	client := &http.Client{Transport: auditRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(make([]byte, 256)))}, nil
	})}
	segs := make([]hlsSegment, 1000)
	for i := range segs {
		segs[i] = hlsSegment{URL: fmt.Sprintf("https://test/%d", i), Seq: i, Duration: 1}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := NewHLSDownloader(client, "https://test/index.m3u8").WithTransportConfig(TransportConfig{MaxConcurrency: 4})
		if err := h.downloadSegmentsConcurrent(context.Background(), segs, io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
