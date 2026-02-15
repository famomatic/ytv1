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

func (s playerResolverStub) GetPlayerJS(context.Context, string) (string, error) {
	return s.js, nil
}

func (s playerResolverStub) GetPlayerURL(context.Context, string) (string, error) {
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

var _ playerjs.Resolver = playerResolverStub{}

