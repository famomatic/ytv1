package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHLSKeySnapshotReuseAndRotation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, strings.Repeat(fmt.Sprint(calls), 16))
	}))
	defer srv.Close()
	h := NewHLSDownloader(srv.Client(), srv.URL+"/list")
	manifest := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXTINF:1,\nseg.ts\n"
	for i := 0; i < 3; i++ {
		if _, _, err := h.parseSegments(context.Background(), manifest, h.PlaylistURL); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("unchanged playlist fetched key %d times", calls)
	}
	segments, _, err := h.parseSegments(context.Background(), strings.Replace(manifest, "seg.ts", "next.ts", 1), h.PlaylistURL)
	if err != nil || calls != 2 || string(segments[0].Key.Key) != strings.Repeat("2", 16) {
		t.Fatalf("same-URI rotation not reloaded: calls=%d err=%v", calls, err)
	}
}
