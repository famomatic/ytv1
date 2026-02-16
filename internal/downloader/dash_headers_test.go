package downloader

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDASHDownloader_PropagatesRequestHeaders(t *testing.T) {
	const headerName = "X-Test-Header"
	const headerValue = "ytv1-dash"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(headerName); got != headerValue {
			http.Error(w, "missing header", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/manifest.mpd":
			w.Header().Set("Content-Type", "application/dash+xml")
			w.Write([]byte(`<?xml version="1.0"?>
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
</MPD>`))
		case "/seg-1.m4s":
			w.Write([]byte("dash-seg"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dl := NewDASHDownloader(server.Client(), server.URL+"/manifest.mpd", "248").WithRequestHeaders(http.Header{
		headerName: []string{headerValue},
	})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != "dash-seg" {
		t.Fatalf("segment payload mismatch: got=%q", got)
	}
}

func TestDASHDownloader_RetriesSegmentFetch(t *testing.T) {
	var segmentCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.mpd":
			w.Header().Set("Content-Type", "application/dash+xml")
			w.Write([]byte(`<?xml version="1.0"?>
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
</MPD>`))
		case "/seg-1.m4s":
			if atomic.AddInt32(&segmentCalls, 1) == 1 {
				http.Error(w, "temporary", http.StatusServiceUnavailable)
				return
			}
			w.Write([]byte("dash-seg"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dl := NewDASHDownloader(server.Client(), server.URL+"/manifest.mpd", "248").WithTransportConfig(TransportConfig{
		MaxRetries:       1,
		InitialBackoff:   time.Millisecond,
		MaxBackoff:       time.Millisecond,
		RetryStatusCodes: []int{http.StatusServiceUnavailable},
	})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != "dash-seg" {
		t.Fatalf("segment payload mismatch: got=%q", got)
	}
	if got := atomic.LoadInt32(&segmentCalls); got != 2 {
		t.Fatalf("segment call count=%d, want 2", got)
	}
}

func TestDASHDownloader_SkipsUnavailableFragmentsInDynamic(t *testing.T) {
	var manifestCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.mpd":
			if atomic.AddInt32(&manifestCalls, 1) == 1 {
				w.Write([]byte(`<?xml version="1.0"?>
<MPD type="dynamic" minimumUpdatePeriod="PT0.01S" xmlns="urn:mpeg:dash:schema:mpd:2011">
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="248" bandwidth="1000000">
        <SegmentTemplate timescale="1" media="seg-$Number$.m4s" startNumber="1">
          <SegmentTimeline>
            <S d="1" r="1"/>
          </SegmentTimeline>
        </SegmentTemplate>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`))
				return
			}
			w.Write([]byte(`<?xml version="1.0"?>
<MPD type="static" xmlns="urn:mpeg:dash:schema:mpd:2011">
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="248" bandwidth="1000000">
        <SegmentTemplate timescale="1" media="seg-$Number$.m4s" startNumber="2">
          <SegmentTimeline>
            <S d="1" r="0"/>
          </SegmentTimeline>
        </SegmentTemplate>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`))
		case "/seg-1.m4s":
			http.NotFound(w, r)
		case "/seg-2.m4s":
			w.Write([]byte("dash-seg"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dl := NewDASHDownloader(server.Client(), server.URL+"/manifest.mpd", "248").WithTransportConfig(TransportConfig{
		SkipUnavailableFragments: true,
		MaxSkippedFragments:      2,
	})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != "dash-seg" {
		t.Fatalf("segment payload mismatch: got=%q", got)
	}
}

func TestDASHDownloader_StaticConcurrentDownloadKeepsOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.mpd":
			w.Write([]byte(`<?xml version="1.0"?>
<MPD type="static" xmlns="urn:mpeg:dash:schema:mpd:2011">
  <Period>
    <AdaptationSet mimeType="video/mp4">
      <Representation id="248" bandwidth="1000000">
        <SegmentTemplate timescale="1" media="seg-$Number$.m4s" startNumber="1">
          <SegmentTimeline>
            <S d="1" r="1"/>
          </SegmentTimeline>
        </SegmentTemplate>
      </Representation>
    </AdaptationSet>
  </Period>
</MPD>`))
		case "/seg-1.m4s":
			w.Write([]byte("A"))
		case "/seg-2.m4s":
			w.Write([]byte("B"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dl := NewDASHDownloader(server.Client(), server.URL+"/manifest.mpd", "248").WithTransportConfig(TransportConfig{
		MaxConcurrency: 4,
	})

	var buf bytes.Buffer
	if err := dl.Download(context.Background(), &buf); err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if got := buf.String(); got != "AB" {
		t.Fatalf("ordered segment payload mismatch: got=%q want=AB", got)
	}
}
