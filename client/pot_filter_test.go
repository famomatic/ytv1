package client

import (
	"errors"
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestFilterFormatsByPoTokenPolicy_RequiredDropsWithoutProvider(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 18, Protocol: "https", HasAudio: true, HasVideo: true},
		{Itag: 251, Protocol: "hls", HasAudio: true},
	}
	cfg := Config{
		PoTokenFetchPolicy: map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy{
			innertube.StreamingProtocolHTTPS: innertube.PoTokenFetchPolicyRequired,
			innertube.StreamingProtocolHLS:   innertube.PoTokenFetchPolicyRecommended,
		},
	}

	kept, skips := filterFormatsByPoTokenPolicy(formats, cfg)
	if len(kept) != 1 || kept[0].Itag != 251 {
		t.Fatalf("unexpected kept formats: %+v", kept)
	}
	if len(skips) != 1 || skips[0].Itag != 18 {
		t.Fatalf("unexpected skip reasons: %+v", skips)
	}
}

func TestDownload_NoPlayableFormatsDetailErrorForPoTokenDrops(t *testing.T) {
	cfg := Config{
		PoTokenFetchPolicy: map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy{
			innertube.StreamingProtocolHTTPS: innertube.PoTokenFetchPolicyRequired,
		},
	}
	_, skips := filterFormatsByPoTokenPolicy([]FormatInfo{{Itag: 18, Protocol: "https", HasAudio: true, HasVideo: true}}, cfg)
	err := &NoPlayableFormatsDetailError{Mode: SelectionModeBest, Skips: skips}
	if !errors.Is(err, ErrNoPlayableFormats) {
		t.Fatalf("expected sentinel compatibility")
	}
	if len(err.Skips) != 1 {
		t.Fatalf("expected one skip reason")
	}
}

func TestFilterFormatsByPoTokenPolicy_UnknownProtocolNotForcedToHTTPS(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 18, Protocol: "unknown", HasAudio: true, HasVideo: true},
	}
	cfg := Config{
		PoTokenFetchPolicy: map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy{
			innertube.StreamingProtocolHTTPS: innertube.PoTokenFetchPolicyRequired,
		},
	}

	kept, skips := filterFormatsByPoTokenPolicy(formats, cfg)
	if len(skips) != 0 {
		t.Fatalf("unexpected skips for unknown protocol: %+v", skips)
	}
	if len(kept) != 1 || kept[0].Itag != 18 {
		t.Fatalf("unexpected kept formats: %+v", kept)
	}
}

func TestFilterFormatsByPoTokenPolicy_SourceClientPolicyDefaultsNonBlocking(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 18, Protocol: "https", SourceClient: "web", HasAudio: true, HasVideo: true},
	}
	cfg := Config{}

	kept, skips := filterFormatsByPoTokenPolicy(formats, cfg)
	if len(skips) != 0 {
		t.Fatalf("unexpected skips for source-client default policy: %+v", skips)
	}
	if len(kept) != 1 || kept[0].Itag != 18 {
		t.Fatalf("unexpected kept formats: %+v", kept)
	}
}

func TestFilterFormatsByPoTokenPolicy_DropsDRMAndDamagedFormats(t *testing.T) {
	formats := []FormatInfo{
		{Itag: 18, Protocol: "https", IsDRM: true, HasAudio: true, HasVideo: true},
		{Itag: 22, Protocol: "https", IsDamaged: true, HasAudio: true, HasVideo: true},
		{Itag: 140, Protocol: "https", HasAudio: true},
	}
	cfg := Config{}

	kept, skips := filterFormatsByPoTokenPolicy(formats, cfg)
	if len(kept) != 1 || kept[0].Itag != 140 {
		t.Fatalf("unexpected kept formats: %+v", kept)
	}
	if len(skips) != 2 {
		t.Fatalf("unexpected skip count: %+v", skips)
	}
	if skips[0].Reason != "drm_protected" || skips[1].Reason != "damaged_format" {
		t.Fatalf("unexpected skip reasons: %+v", skips)
	}
}
