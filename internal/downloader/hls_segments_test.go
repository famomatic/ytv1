package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHLSMediaSegmentReaderFollowsLiveRefresh(t *testing.T) {
	var manifestReads atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/live.m3u8":
			read := manifestReads.Add(1)
			sequence := 10
			segment := "10.ts"
			if read > 1 {
				sequence, segment = 11, "11.ts"
			}
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXTINF:1,\n%s\n", sequence, segment)
		case "/10.ts":
			_, _ = w.Write([]byte("ten"))
		case "/11.ts":
			_, _ = w.Write([]byte("eleven"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reader := NewHLSMediaSegmentReader(srv.Client(), srv.URL+"/live.m3u8")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first, err := reader.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 10 || string(first.Data) != "ten" {
		t.Fatalf("first segment = seq:%d data:%q", first.Sequence, first.Data)
	}
	if second.Sequence != 11 || string(second.Data) != "eleven" {
		t.Fatalf("second segment = seq:%d data:%q", second.Sequence, second.Data)
	}
}

func TestHLSMediaSegmentReaderStartsNearLiveEdge(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/live.m3u8" {
			_, _ = fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:5\n#EXT-X-MEDIA-SEQUENCE:20\n#EXTINF:5,\n20.ts\n#EXTINF:5,\n21.ts\n#EXTINF:5,\n22.ts\n#EXTINF:5,\n23.ts\n")
			return
		}
		_, _ = fmt.Fprint(w, r.URL.Path)
	}))
	defer srv.Close()

	reader := NewHLSMediaSegmentReader(srv.Client(), srv.URL+"/live.m3u8").WithLiveEdgeSegments(2)
	segment, err := reader.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if segment.Sequence != 22 || string(segment.Data) != "/22.ts" {
		t.Fatalf("first segment = seq:%d data:%q, want live-edge sequence 22", segment.Sequence, segment.Data)
	}
}
