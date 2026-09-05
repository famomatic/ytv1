package client

import (
	"github.com/famomatic/ytv1/internal/innertube"
	"net/url"
	"testing"
	"time"
)

func TestSessionExpiryCoversAllAddressing(t *testing.T) {
	raw := "https://media.test/x?expire=1"
	for _, sess := range []videoSession{
		{Response: &innertube.PlayerResponse{StreamingData: innertube.StreamingData{DashManifestURL: raw}}},
		{Response: &innertube.PlayerResponse{StreamingData: innertube.StreamingData{Formats: []innertube.Format{{SignatureCipher: "url=" + url.QueryEscape(raw)}}}}},
		{Info: &VideoInfo{Formats: []FormatInfo{{Parts: []string{raw}}}}},
	} {
		if !sessionExpired(sess, time.Now()) {
			t.Fatal("expired addressing accepted")
		}
	}
}
