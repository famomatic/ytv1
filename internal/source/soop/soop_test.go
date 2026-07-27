package soop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMatches(t *testing.T) {
	s := New(nil)
	cases := []struct {
		in   string
		want bool
	}{
		{"https://vod.sooplive.co.kr/player/177615269", true},
		{"https://vod.sooplive.com/player/201581319", true},
		{"https://play.sooplive.com/b13246/290898914", true},
		{"https://play.sooplive.co.kr/b13246/290898914", true},
		{"https://play.sooplive.co.kr/b13246", true},
		{"http://www.play.sooplive.co.kr/b13246/1", true},
		{"https://www.youtube.com/watch?v=abcdefghijk", false},
		{"https://vod.sooplive.co.kr/", false},
		{"random text", false},
	}
	for _, c := range cases {
		if got := s.Matches(c.in); got != c.want {
			t.Errorf("Matches(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// masterPlaylist is a two-variant HLS master playlist.
func masterPlaylist(base string) string {
	return strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080,FRAME-RATE=60",
		base + "/1080.m3u8",
		"#EXT-X-STREAM-INF:BANDWIDTH=1500000,RESOLUTION=1280x720,FRAME-RATE=30",
		base + "/720.m3u8",
		"",
	}, "\n")
}

func TestExtractVOD(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/station/video/a/view":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if got := r.PostForm.Get("nTitleNo"); got != "177615269" {
				t.Errorf("nTitleNo = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"result":1,"data":{"title":"Test VOD","writer_nick":"Streamer","files":[{"file":%q,"duration":123000,"snapshot":"http://img/x.jpg"}]}}`, srv.URL+"/master.m3u8")
		case "/master.m3u8":
			fmt.Fprint(w, masterPlaylist(srv.URL))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	old := vodViewAPI
	vodViewAPI = srv.URL + "/station/video/a/view"
	defer func() { vodViewAPI = old }()

	s := New(srv.Client())
	media, err := s.Extract(context.Background(), "https://vod.sooplive.co.kr/player/177615269")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if media.ID != "177615269" || media.Title != "Test VOD" || media.Author != "Streamer" {
		t.Errorf("metadata mismatch: %+v", media)
	}
	if media.IsLive {
		t.Error("VOD flagged as live")
	}
	if media.DurationSec != 123 {
		t.Errorf("duration = %d, want 123", media.DurationSec)
	}
	if len(media.Formats) != 2 {
		t.Fatalf("got %d formats, want 2", len(media.Formats))
	}
	// Highest-quality variant present and tagged.
	var have1080 bool
	for _, f := range media.Formats {
		if f.Height == 1080 {
			have1080 = true
		}
		if f.SourceClient != "soop" || f.Protocol != "hls" {
			t.Errorf("format not tagged as soop/hls: %+v", f)
		}
	}
	if !have1080 {
		t.Error("missing 1080p variant")
	}
	if media.MediaHeaders.Get("Origin") != originHeader {
		t.Errorf("Origin header = %q", media.MediaHeaders.Get("Origin"))
	}
	if media.MediaHeaders.Get("Referer") == "" {
		t.Error("missing Referer header")
	}
}

func TestExtractVODMultiPart(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/station/video/a/view":
			_ = r.ParseForm()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"result":1,"data":{"title":"Multi VOD","writer_nick":"Streamer","files":[`+
				`{"file":%q,"duration":100000,"snapshot":"http://img/a.jpg"},`+
				`{"file":%q,"duration":50000,"snapshot":"http://img/b.jpg"}]}}`,
				srv.URL+"/p0/master.m3u8", srv.URL+"/p1/master.m3u8")
		case "/p0/master.m3u8":
			fmt.Fprint(w, masterPlaylist(srv.URL+"/p0"))
		case "/p1/master.m3u8":
			fmt.Fprint(w, masterPlaylist(srv.URL+"/p1"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	old := vodViewAPI
	vodViewAPI = srv.URL + "/station/video/a/view"
	defer func() { vodViewAPI = old }()

	s := New(srv.Client())
	media, err := s.Extract(context.Background(), "https://vod.sooplive.co.kr/player/177615269")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Total duration sums every part (100s + 50s).
	if media.DurationSec != 150 {
		t.Errorf("duration = %d, want 150", media.DurationSec)
	}
	if len(media.Formats) != 2 {
		t.Fatalf("got %d formats, want 2", len(media.Formats))
	}
	for _, f := range media.Formats {
		// Every quality resolves to both parts, in order, height-matched.
		if len(f.Parts) != 2 {
			t.Fatalf("format h=%d: got %d parts, want 2 (%v)", f.Height, len(f.Parts), f.Parts)
		}
		if f.Parts[0] != f.URL {
			t.Errorf("Parts[0]=%q != URL=%q", f.Parts[0], f.URL)
		}
		wantP0 := fmt.Sprintf("%s/p0/%d.m3u8", srv.URL, f.Height)
		wantP1 := fmt.Sprintf("%s/p1/%d.m3u8", srv.URL, f.Height)
		if f.Parts[0] != wantP0 || f.Parts[1] != wantP1 {
			t.Errorf("format h=%d parts = %v, want [%s %s]", f.Height, f.Parts, wantP0, wantP1)
		}
	}
}

func TestExtractLive(t *testing.T) {
	// Force the public CDN path: this test exercises Route C/540p, not the local
	// P2P agent (which may be running on the dev machine).
	defer func(p func() bool) { agentProbe = p }(agentProbe)
	agentProbe = func() bool { return false }

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/player_live_api.php":
			_ = r.ParseForm()
			if r.PostForm.Get("type") != "aid" {
				t.Errorf("live api type = %q, want aid", r.PostForm.Get("type"))
			}
			fmt.Fprint(w, `{"CHANNEL":{"TITLE":"Live Now","BJNICK":"BJ","COLONY_CONTENT":"AUTHKEY123"}}`)
		case "/broad_stream_assign.html":
			if got := r.URL.Query().Get("broad_key"); got != "290898914-common-master-hls" {
				t.Errorf("broad_key = %q", got)
			}
			fmt.Fprintf(w, `{"view_url":%q}`, srv.URL+"/live/auth_playlist.m3u8")
		case "/live/auth_master_playlist.m3u8":
			if got := r.URL.Query().Get("aid"); got != "AUTHKEY123" {
				t.Errorf("aid = %q, want AUTHKEY123", got)
			}
			fmt.Fprint(w, masterPlaylist(srv.URL+"/live"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	oldLive, oldAssign := liveInfoAPI, streamAssignAPI
	liveInfoAPI = srv.URL + "/player_live_api.php"
	streamAssignAPI = srv.URL + "/broad_stream_assign.html"
	defer func() { liveInfoAPI, streamAssignAPI = oldLive, oldAssign }()

	s := New(srv.Client())
	media, err := s.Extract(context.Background(), "https://play.sooplive.co.kr/b13246/290898914")
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !media.IsLive {
		t.Error("live not flagged")
	}
	if media.ID != "290898914" || media.Title != "Live Now" || media.Author != "BJ" {
		t.Errorf("metadata mismatch: %+v", media)
	}
	if len(media.Formats) != 2 {
		t.Fatalf("got %d formats, want 2", len(media.Formats))
	}
}
