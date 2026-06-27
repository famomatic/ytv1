package client

import (
	"path/filepath"
	"testing"
)

func TestEffectiveOutputTemplateOutputPathDir(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		opts OutputTemplateOptions
		want string
	}{
		{name: "default template under dir", opts: OutputTemplateOptions{OutputPathDir: dir}, want: filepath.Join(dir, "%(title)s [%(id)s].%(ext)s")},
		{name: "relative template under dir", opts: OutputTemplateOptions{OutputPathDir: dir, OutputTemplate: "%(title)s.%(ext)s"}, want: filepath.Join(dir, "%(title)s.%(ext)s")},
		{name: "absolute template unchanged", opts: OutputTemplateOptions{OutputPathDir: "ignored", OutputTemplate: filepath.Join(dir, "fixed.%(ext)s")}, want: filepath.Join(dir, "fixed.%(ext)s")},
		{name: "id shortcut under dir", opts: OutputTemplateOptions{OutputPathDir: dir, OutputUseID: true}, want: filepath.Join(dir, "%(id)s.%(ext)s")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveOutputTemplate(tc.opts); got != tc.want {
				t.Fatalf("EffectiveOutputTemplate() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildDownloadOptionsAliases(t *testing.T) {
	got := BuildDownloadOptions(DownloadOptionsRequest{
		FormatSelector: "bestvideo+bestaudio",
		OutputTemplate: "x.mp4",
		Resume:         true,
		UsePartFiles:   true,
	})
	if got.FormatSelector != "bestvideo+bestaudio/best" {
		t.Fatalf("FormatSelector=%q", got.FormatSelector)
	}
	if got.Mode != SelectionModeBest || got.OutputPath != "x.mp4" || !got.Resume || !got.UsePartFiles {
		t.Fatalf("unexpected options: %+v", got)
	}

	got = BuildDownloadOptions(DownloadOptionsRequest{ExtractAudio: true, AudioFormat: "mp3"})
	if got.Mode != SelectionModeMP3 {
		t.Fatalf("extract-audio mp3 mode=%q, want mp3", got.Mode)
	}

	got = BuildDownloadOptions(DownloadOptionsRequest{FormatSelector: "140"})
	if got.Itag != 140 {
		t.Fatalf("itag=%d, want 140", got.Itag)
	}
}
