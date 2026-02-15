package client

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/playerjs"
)

type playerResolverStub struct {
	js string
}

type countingPlayerResolverStub struct {
	js    string
	calls int
}

func (s playerResolverStub) GetPlayerJS(context.Context, string) (string, error) {
	return s.js, nil
}

func (s playerResolverStub) GetPlayerURL(context.Context, string) (string, error) {
	return "/s/player/test/base.js", nil
}

func (s *countingPlayerResolverStub) GetPlayerJS(context.Context, string) (string, error) {
	s.calls++
	return s.js, nil
}

func (s *countingPlayerResolverStub) GetPlayerURL(context.Context, string) (string, error) {
	return "/s/player/test/base.js", nil
}

func testClientWithSession(videoID string, format innertube.Format, js string) *Client {
	resp := &innertube.PlayerResponse{
		VideoDetails: innertube.VideoDetails{VideoID: videoID},
		StreamingData: innertube.StreamingData{
			AdaptiveFormats: []innertube.Format{format},
		},
	}
	return &Client{
		config:           Config{HTTPClient: http.DefaultClient},
		playerJSResolver: playerResolverStub{js: js},
		sessions: map[string]videoSession{
			videoID: {
				Response:  resp,
				PlayerURL: "/s/player/test/base.js",
			},
		},
	}
}

func buildCipher(rawURL string, pairs map[string]string) string {
	v := url.Values{}
	v.Set("url", rawURL)
	for k, value := range pairs {
		v.Set(k, value)
	}
	return v.Encode()
}

func testPlayerJS() string {
	return `
var AB={c:function(a,b){a.splice(0,b)}};
function ZZ(a){a=a.split("");a=AB.c(a,1);return a.join("")}
xx.get("n"))&&(b=abc[0](x)+1||nx)
;nx=function(a){return a.slice(1)}
`
}

func TestResolveStreamURL_SOnly(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	format := innertube.Format{
		Itag: 251,
		SignatureCipher: buildCipher("https://example.com/audio?foo=1", map[string]string{
			"s":  "xyz",
			"sp": "sig",
		}),
	}
	c := testClientWithSession(videoID, format, testPlayerJS())

	out, err := c.ResolveStreamURL(context.Background(), videoID, 251)
	if err != nil {
		t.Fatalf("ResolveStreamURL() error = %v", err)
	}
	u, _ := url.Parse(out)
	if got := u.Query().Get("sig"); got != "yz" {
		t.Fatalf("sig = %q, want %q", got, "yz")
	}
}

func TestResolveStreamURL_NOnly(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	format := innertube.Format{
		Itag: 140,
		SignatureCipher: buildCipher("https://example.com/audio?n=abcd&foo=1", nil),
	}
	c := testClientWithSession(videoID, format, testPlayerJS())

	out, err := c.ResolveStreamURL(context.Background(), videoID, 140)
	if err != nil {
		t.Fatalf("ResolveStreamURL() error = %v", err)
	}
	u, _ := url.Parse(out)
	if got := u.Query().Get("n"); got != "bcd" {
		t.Fatalf("n = %q, want %q", got, "bcd")
	}
}

func TestResolveStreamURL_SAndN(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	format := innertube.Format{
		Itag: 250,
		SignatureCipher: buildCipher("https://example.com/audio?n=abcd", map[string]string{
			"s":  "xyz",
			"sp": "signature",
		}),
	}
	c := testClientWithSession(videoID, format, testPlayerJS())

	out, err := c.ResolveStreamURL(context.Background(), videoID, 250)
	if err != nil {
		t.Fatalf("ResolveStreamURL() error = %v", err)
	}
	u, _ := url.Parse(out)
	if got := u.Query().Get("signature"); got != "yz" {
		t.Fatalf("signature = %q, want %q", got, "yz")
	}
	if got := u.Query().Get("n"); got != "bcd" {
		t.Fatalf("n = %q, want %q", got, "bcd")
	}
}

func TestResolveStreamURL_DirectURLWithN(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	format := innertube.Format{
		Itag: 18,
		URL:  "https://example.com/video?n=abcd&foo=1",
	}
	c := testClientWithSession(videoID, format, testPlayerJS())

	out, err := c.ResolveStreamURL(context.Background(), videoID, 18)
	if err != nil {
		t.Fatalf("ResolveStreamURL() error = %v", err)
	}
	u, _ := url.Parse(out)
	if got := u.Query().Get("n"); got != "bcd" {
		t.Fatalf("n = %q, want %q", got, "bcd")
	}
}

func TestResolveStreamURL_DirectURLWithNWithoutPlayerURL(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	format := innertube.Format{
		Itag: 18,
		URL:  "https://example.com/video?n=abcd&foo=1",
	}
	c := testClientWithSession(videoID, format, testPlayerJS())
	session := c.sessions[videoID]
	session.PlayerURL = ""
	c.sessions[videoID] = session

	_, err := c.ResolveStreamURL(context.Background(), videoID, 18)
	if err != ErrChallengeNotSolved {
		t.Fatalf("ResolveStreamURL() error = %v, want %v", err, ErrChallengeNotSolved)
	}
}

func TestResolveStreamURL_MalformedCipher(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	format := innertube.Format{
		Itag:            249,
		SignatureCipher: "%zz",
	}
	c := testClientWithSession(videoID, format, testPlayerJS())

	_, err := c.ResolveStreamURL(context.Background(), videoID, 249)
	if err != ErrChallengeNotSolved {
		t.Fatalf("ResolveStreamURL() error = %v, want %v", err, ErrChallengeNotSolved)
	}
}

func TestResolveSelectedFormatURL_PrefersSelectedFormatURL(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	c := testClientWithSession(videoID, innertube.Format{
		Itag: 248,
		URL:  "https://blocked.example/video",
	}, testPlayerJS())

	selected := FormatInfo{
		Itag: 248,
		URL:  "https://manifest.example/video",
	}

	out, err := c.resolveSelectedFormatURL(context.Background(), videoID, selected)
	if err != nil {
		t.Fatalf("resolveSelectedFormatURL() error = %v", err)
	}
	if out != "https://manifest.example/video" {
		t.Fatalf("resolveSelectedFormatURL()=%q, want %q", out, "https://manifest.example/video")
	}
}

func TestPrimeChallengeSolutions_BatchesAndCaches(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	resolver := &countingPlayerResolverStub{js: testPlayerJS()}
	resp := &innertube.PlayerResponse{
		VideoDetails: innertube.VideoDetails{VideoID: videoID},
		StreamingData: innertube.StreamingData{
			AdaptiveFormats: []innertube.Format{
				{
					Itag:            251,
					SignatureCipher: buildCipher("https://example.com/audio?n=abcd", map[string]string{"s": "xyz", "sp": "sig"}),
				},
			},
		},
	}
	c := &Client{
		config:           Config{HTTPClient: http.DefaultClient},
		playerJSResolver: resolver,
		sessions:         map[string]videoSession{},
		challenges:       map[string]challengeSolutions{},
	}

	c.primeChallengeSolutions(context.Background(), "/s/player/test/base.js", resp, "", "")
	if resolver.calls != 1 {
		t.Fatalf("player js calls=%d, want 1", resolver.calls)
	}

	if _, err := c.decodeSignatureWithCache(context.Background(), "/s/player/test/base.js", "xyz"); err != nil {
		t.Fatalf("decodeSignatureWithCache() error = %v", err)
	}
	if _, err := c.decodeNWithCache(context.Background(), "/s/player/test/base.js", "abcd"); err != nil {
		t.Fatalf("decodeNWithCache() error = %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("expected cached decode calls to avoid extra js fetch, got calls=%d", resolver.calls)
	}
}

func TestPrimeChallengeSolutions_EmitsPartialOnMixedSolve(t *testing.T) {
	videoID := "jNQXAC9IVRw"
	// n-function exists, signature decipher ops intentionally absent.
	js := `
xx.get("n"))&&(b=abc[0](x)+1||nx)
;nx=function(a){return a.slice(1)}
`
	resolver := &countingPlayerResolverStub{js: js}
	resp := &innertube.PlayerResponse{
		VideoDetails: innertube.VideoDetails{VideoID: videoID},
		StreamingData: innertube.StreamingData{
			AdaptiveFormats: []innertube.Format{
				{
					Itag:            251,
					SignatureCipher: buildCipher("https://example.com/audio?n=abcd", map[string]string{"s": "xyz", "sp": "sig"}),
				},
			},
		},
	}
	var phases []string
	c := &Client{
		config: Config{
			HTTPClient: http.DefaultClient,
			OnExtractionEvent: func(evt ExtractionEvent) {
				if evt.Stage == "challenge" {
					phases = append(phases, evt.Phase)
				}
			},
		},
		playerJSResolver: resolver,
		sessions:         map[string]videoSession{},
		challenges:       map[string]challengeSolutions{},
	}

	c.primeChallengeSolutions(context.Background(), "/s/player/test/base.js", resp, "", "")
	if len(phases) == 0 {
		t.Fatalf("expected challenge events")
	}
	last := phases[len(phases)-1]
	if last != "partial" {
		t.Fatalf("last challenge phase=%q, want partial", last)
	}
}

var _ playerjs.Resolver = playerResolverStub{}
var _ playerjs.Resolver = (*countingPlayerResolverStub)(nil)

