package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAudit20260906RedirectBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/dir/list.m3u8", 302)
		case "/dir/list.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXTINF:1,\nseg.ts\n#EXT-X-ENDLIST\n")
		case "/dir/seg.ts":
			fmt.Fprint(w, "media")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	e := NewHLSDownloader(srv.Client(), srv.URL+"/start").Download(context.Background(), &out)
	if e != nil {
		t.Fatalf("regression: %v", e)
	}

}
func TestAudit20260906SegmentReaderIgnoresSkip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list" {
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nmissing\n#EXTINF:1,\ngood\n")
		} else if r.URL.Path == "/good" {
			fmt.Fprint(w, "media")
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := NewHLSMediaSegmentReader(srv.Client(), srv.URL+"/list").WithTransportConfig(TransportConfig{SkipUnavailableFragments: true, MaxSkippedFragments: 1})
	_, e := r.Next(context.Background())
	if e != nil {
		t.Fatalf("regression: %v", e)
	}

}
func TestAudit20260906AdaptiveSkipLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sq") == "" {
			w.Header().Set("X-Head-Seqnum", "5")
		} else {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var out bytes.Buffer
	e := NewLiveAdaptiveDownloader(srv.Client(), srv.URL).WithTransportConfig(TransportConfig{SkipUnavailableFragments: true, MaxSkippedFragments: 1}).Download(context.Background(), &out)
	if e == nil || !strings.Contains(e.Error(), "skip limit") {
		t.Fatalf("regression: %v", e)
	}

}
func TestAudit20260906DASHLiveWindow(t *testing.T) {
	raw := fmt.Sprintf(`<MPD type="dynamic" availabilityStartTime="%s" timeShiftBufferDepth="PT30S"><Period><AdaptationSet><Representation id="v"><SegmentTemplate duration="2" media="$Number$.m4s"/></Representation></AdaptationSet></Period></MPD>`, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	p, e := parseDASH([]byte(raw))
	if e != nil {
		t.Fatal(e)
	}
	s, _, e := NewDASHDownloader(nil, "https://example.test/list.mpd", "v").extractSegments(p)
	if e != nil || len(s) < 14 || len(s) > 16 || strings.HasSuffix(s[0].URL, "/1.m4s") {
		t.Fatalf("regression: len=%d %v", len(s), e)
	}

}
func TestAudit20260906DASHSegmentListIgnored(t *testing.T) {
	p, e := parseDASH([]byte(`<MPD><Period><AdaptationSet><Representation id="v"><BaseURL>media/</BaseURL><SegmentList><Initialization sourceURL="init"/><SegmentURL media="seg"/></SegmentList></Representation></AdaptationSet></Period></MPD>`))
	if e != nil {
		t.Fatal(e)
	}
	s, _, e := NewDASHDownloader(nil, "https://example.test/list.mpd", "v").extractSegments(p)
	if e == nil || !strings.Contains(e.Error(), "unsupported") {
		t.Fatalf("regression: %+v %v", s, e)
	}

}
