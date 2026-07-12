package client

import "testing"

func TestRenderOutputTemplate_CommonTokens(t *testing.T) {
	got := RenderOutputTemplate("%(title)s-%(id)s-%(format_id)s-%(vcodec)s.%(ext)s", OutputTemplateData{
		VideoID:  "abc123",
		Title:    "bad/name",
		Itag:     "248",
		VCodec:   "vp9",
		Ext:      "webm",
		FormatID: "248",
	})
	want := "bad_name-abc123-248-vp9.webm"
	if got != want {
		t.Fatalf("RenderOutputTemplate()=%q, want %q", got, want)
	}
}

func TestRenderOutputTemplate_RestrictedFilenames(t *testing.T) {
	got := RenderOutputTemplate("%(title)s", OutputTemplateData{
		Title:             "A title with 한글 / symbols!",
		RestrictFilenames: true,
	})
	if got != "A_title_with_symbols" {
		t.Fatalf("RenderOutputTemplate()=%q, want A_title_with_symbols", got)
	}
}

func TestSelectedFormatTemplateTokens(t *testing.T) {
	tokens := SelectedFormatTemplateTokens([]FormatInfo{
		{
			Itag:     248,
			MimeType: "video/webm; codecs=\"vp9\"",
			HasVideo: true,
			Width:    1920,
			Height:   1080,
			FPS:      60,
			Bitrate:  2000000,
			Protocol: "https",
		},
		{
			Itag:     251,
			MimeType: "audio/webm; codecs=\"opus\"",
			HasAudio: true,
			Bitrate:  128000,
			Protocol: "https",
		},
	})
	if tokens.Resolution != "1920x1080" || tokens.Width != "1920" || tokens.Height != "1080" || tokens.FPS != "60" {
		t.Fatalf("video tokens=%+v", tokens)
	}
	if tokens.TBR != "2128" || tokens.VBR != "2000" || tokens.ABR != "128" {
		t.Fatalf("bitrate tokens=%+v", tokens)
	}
	if tokens.VCodec != "vp9" || tokens.ACodec != "opus" || tokens.Protocol != "https" {
		t.Fatalf("codec/protocol tokens=%+v", tokens)
	}
}

func TestSanitizeOutputToken_WindowsReservedNames(t *testing.T) {
	cases := map[string]string{
		"CON":     "unknown",
		"con":     "unknown",
		"PRN.txt": "unknown",
		"AUX":     "unknown",
		"NUL":     "unknown",
		"COM1":    "unknown",
		"LPT9":    "unknown",
		"normal":  "normal",
		"video":   "video",
	}
	for input, want := range cases {
		got := SanitizeOutputTemplateToken(input)
		if got != want {
			t.Errorf("SanitizeOutputTemplateToken(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestSanitizeOutputToken_TrailingDotsAndDotsOnly(t *testing.T) {
	cases := map[string]string{
		"foo.":    "foo",
		"foo..":   "foo",
		"foo ":    "foo",
		".":       "unknown",
		"..":      "unknown",
		"   ":     "unknown",
		"trail. ": "trail",
	}
	for input, want := range cases {
		got := SanitizeOutputTemplateToken(input)
		if got != want {
			t.Errorf("SanitizeOutputTemplateToken(%q)=%q, want %q", input, got, want)
		}
	}
}
