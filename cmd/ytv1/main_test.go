package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
)

func TestFormatExtractionEvent(t *testing.T) {
	got := formatExtractionEvent(client.ExtractionEvent{
		Stage:  "player_api_json",
		Phase:  "start",
		Client: "mweb",
		Detail: "attempt=1",
	})
	want := "[extract] player_api_json:start client=mweb detail=attempt=1"
	if got != want {
		t.Fatalf("formatExtractionEvent()=%q want=%q", got, want)
	}
}

func TestFormatDownloadEvent(t *testing.T) {
	got := formatDownloadEvent(client.DownloadEvent{
		Stage:   "merge",
		Phase:   "complete",
		VideoID: "DSYFmhjDbvs",
		Path:    "out.webm",
		Detail:  "ok",
	})
	want := "[download] merge:complete video_id=DSYFmhjDbvs path=out.webm detail=ok"
	if got != want {
		t.Fatalf("formatDownloadEvent()=%q want=%q", got, want)
	}
}

func TestLifecyclePrinter_ExtractionElapsed(t *testing.T) {
	clock := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, int64(125*time.Millisecond)),
	}
	i := 0
	lp := newLifecyclePrinter(func() time.Time {
		v := clock[i]
		i++
		return v
	})

	_ = lp.formatExtractionEvent(client.ExtractionEvent{
		Stage:  "player_api_json",
		Phase:  "start",
		Client: "web",
	})
	got := lp.formatExtractionEvent(client.ExtractionEvent{
		Stage:  "player_api_json",
		Phase:  "success",
		Client: "web",
		Detail: "ok",
	})
	if !strings.Contains(got, "elapsed_ms=125") {
		t.Fatalf("expected elapsed_ms in output: %q", got)
	}
}

func TestLifecyclePrinter_DownloadElapsedAndSpeed(t *testing.T) {
	clock := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, int64(2*time.Second)),
	}
	i := 0
	lp := newLifecyclePrinter(func() time.Time {
		v := clock[i]
		i++
		return v
	})

	_ = lp.formatDownloadEvent(client.DownloadEvent{
		Stage:   "download",
		Phase:   "start",
		VideoID: "x",
		Path:    "x.f248.video",
		Detail:  "itag=248",
	})
	got := lp.formatDownloadEvent(client.DownloadEvent{
		Stage:   "download",
		Phase:   "complete",
		VideoID: "x",
		Path:    "x.f248.video",
		Detail:  "bytes=10485760",
	})
	if !strings.Contains(got, "elapsed_ms=2000") {
		t.Fatalf("expected elapsed_ms in output: %q", got)
	}
	if !strings.Contains(got, "speed_bps=") || !strings.Contains(got, "speed_mib_s=") {
		t.Fatalf("expected speed fields in output: %q", got)
	}
	if !strings.Contains(got, "part=video") {
		t.Fatalf("expected role field in output: %q", got)
	}
}

func TestBuildDownloadOptions_CustomSelectorPassthrough(t *testing.T) {
	got := buildDownloadOptions(cli.Options{
		FormatSelector: "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best",
		OutputTemplate: "x.mp4",
	})
	if got.FormatSelector != "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best" {
		t.Fatalf("FormatSelector = %q", got.FormatSelector)
	}
	if got.Mode != client.SelectionModeBest {
		t.Fatalf("Mode = %q, want %q", got.Mode, client.SelectionModeBest)
	}
	if got.OutputPath != "x.mp4" {
		t.Fatalf("OutputPath = %q", got.OutputPath)
	}
}

func TestEffectiveOutputTemplate_OutputPathDir(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		opts cli.Options
		want string
	}{
		{
			name: "default template under dir",
			opts: cli.Options{OutputPathDir: dir},
			want: filepath.Join(dir, "%(title)s [%(id)s].%(ext)s"),
		},
		{
			name: "relative template under dir",
			opts: cli.Options{OutputPathDir: dir, OutputTemplate: "%(title)s.%(ext)s"},
			want: filepath.Join(dir, "%(title)s.%(ext)s"),
		},
		{
			name: "absolute template unchanged",
			opts: cli.Options{OutputPathDir: "ignored", OutputTemplate: filepath.Join(dir, "fixed.%(ext)s")},
			want: filepath.Join(dir, "fixed.%(ext)s"),
		},
		{
			name: "id shortcut under dir",
			opts: cli.Options{OutputPathDir: dir, OutputUseID: true},
			want: filepath.Join(dir, "%(id)s.%(ext)s"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveOutputTemplate(tc.opts); got != tc.want {
				t.Fatalf("effectiveOutputTemplate() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildDownloadOptions_OutputPathDir(t *testing.T) {
	dir := t.TempDir()
	got := buildDownloadOptions(cli.Options{OutputPathDir: dir})
	want := filepath.Join(dir, "%(title)s [%(id)s].%(ext)s")
	if got.OutputPath != want {
		t.Fatalf("OutputPath = %q, want %q", got.OutputPath, want)
	}
}

func TestBuildDownloadOptionsForVideo_UsesPredictedConcretePath(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "A title / 한국어?",
		Formats: []client.FormatInfo{
			{Itag: 18, MimeType: "video/mp4", HasAudio: true, HasVideo: true},
		},
	}
	got, err := buildDownloadOptionsForVideo(info, cli.Options{
		OutputTemplate:    "%(title)s.%(ext)s",
		RestrictFilenames: true,
		TrimFilenames:     8,
	})
	if err != nil {
		t.Fatalf("buildDownloadOptionsForVideo() error = %v", err)
	}
	if got.OutputPath != "A_title.mp4" {
		t.Fatalf("OutputPath=%q, want A_title.mp4", got.OutputPath)
	}
}

func TestShouldSkipExistingOutput_NoOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.mp4")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write existing: %v", err)
	}
	if !shouldSkipExistingOutput(path, cli.Options{NoOverwrites: true}) {
		t.Fatalf("expected existing output to be skipped")
	}
	if shouldSkipExistingOutput(path, cli.Options{}) {
		t.Fatalf("expected default overwrite policy not to skip")
	}
	if shouldSkipExistingOutput(filepath.Join(dir, "missing.mp4"), cli.Options{NoOverwrites: true}) {
		t.Fatalf("expected missing output not to be skipped")
	}
}

func TestShouldSkipExistingPostprocessedOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.mp4")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	info := &client.VideoInfo{
		ID: "jNQXAC9IVRw",
		Formats: []client.FormatInfo{
			{Itag: 137, MimeType: "video/mp4", HasVideo: true},
			{Itag: 140, MimeType: "audio/mp4", HasAudio: true},
		},
	}
	if !shouldSkipExistingPostprocessedOutput(info, path, cli.Options{
		NoPostOverwrites: true,
		FormatSelector:   "bestvideo+bestaudio",
	}) {
		t.Fatalf("expected merged output skip under --no-post-overwrites")
	}
	if shouldSkipExistingPostprocessedOutput(&client.VideoInfo{
		ID:      "jNQXAC9IVRw",
		Formats: []client.FormatInfo{{Itag: 18, MimeType: "video/mp4", HasVideo: true, HasAudio: true}},
	}, path, cli.Options{
		NoPostOverwrites: true,
		FormatSelector:   "18",
	}) {
		t.Fatalf("direct downloads should not use post-overwrite policy")
	}
	if shouldSkipExistingPostprocessedOutput(info, path, cli.Options{
		NoPostOverwrites: false,
		FormatSelector:   "bestvideo+bestaudio",
	}) {
		t.Fatalf("post-overwrites enabled should not skip")
	}
}

func TestShouldSkipExistingPostprocessedOutput_MP3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.mp3")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if !shouldSkipExistingPostprocessedOutput(nil, path, cli.Options{
		NoPostOverwrites: true,
		FormatSelector:   "mp3",
	}) {
		t.Fatalf("expected mp3 output skip under --no-post-overwrites")
	}
}

func TestSleepBeforeMediaDownload(t *testing.T) {
	prev := sleepBeforeDownloadFunc
	defer func() { sleepBeforeDownloadFunc = prev }()
	var got time.Duration
	sleepBeforeDownloadFunc = func(d time.Duration) { got = d }

	err := sleepBeforeMediaDownload(context.Background(), cli.Options{SleepInterval: 1500 * time.Millisecond})
	if err != nil {
		t.Fatalf("sleepBeforeMediaDownload() error = %v", err)
	}
	if got != 1500*time.Millisecond {
		t.Fatalf("sleep duration=%s, want 1.5s", got)
	}
}

func TestSleepBeforeExtractionRequest(t *testing.T) {
	prev := sleepBeforeRequestFunc
	defer func() { sleepBeforeRequestFunc = prev }()
	var got time.Duration
	sleepBeforeRequestFunc = func(d time.Duration) { got = d }

	err := sleepBeforeExtractionRequest(context.Background(), cli.Options{SleepRequests: 750 * time.Millisecond})
	if err != nil {
		t.Fatalf("sleepBeforeExtractionRequest() error = %v", err)
	}
	if got != 750*time.Millisecond {
		t.Fatalf("sleep duration=%s, want 750ms", got)
	}
}

func TestSleepBeforeSubtitleDownload(t *testing.T) {
	prev := sleepBeforeSubtitleFunc
	defer func() { sleepBeforeSubtitleFunc = prev }()
	var got time.Duration
	sleepBeforeSubtitleFunc = func(d time.Duration) { got = d }

	err := sleepBeforeSubtitleDownload(context.Background(), cli.Options{SleepSubtitles: 250 * time.Millisecond})
	if err != nil {
		t.Fatalf("sleepBeforeSubtitleDownload() error = %v", err)
	}
	if got != 250*time.Millisecond {
		t.Fatalf("sleep duration=%s, want 250ms", got)
	}
}

func TestMediaFileMTime(t *testing.T) {
	got, ok := client.MediaFileMTime(&client.VideoInfo{UploadDate: "2005-04-23"})
	if !ok {
		t.Fatalf("client.MediaFileMTime() ok=false, want true")
	}
	if got.Format("2006-01-02") != "2005-04-23" {
		t.Fatalf("mtime=%s, want 2005-04-23", got.Format("2006-01-02"))
	}
	got, ok = client.MediaFileMTime(&client.VideoInfo{PublishDate: "20050424"})
	if !ok {
		t.Fatalf("client.MediaFileMTime compact ok=false, want true")
	}
	if got.Format("2006-01-02") != "2005-04-24" {
		t.Fatalf("mtime=%s, want 2005-04-24", got.Format("2006-01-02"))
	}
	if _, ok := client.MediaFileMTime(&client.VideoInfo{UploadDate: "not-a-date"}); ok {
		t.Fatalf("client.MediaFileMTime invalid ok=true, want false")
	}
}

func TestApplyDownloadedFileMTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := applyDownloadedFileMTime(path, &client.VideoInfo{UploadDate: "2005-04-23"}, cli.Options{UpdateMTime: true}); err != nil {
		t.Fatalf("applyDownloadedFileMTime() error = %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if st.ModTime().UTC().Format("2006-01-02") != "2005-04-23" {
		t.Fatalf("modtime=%s, want 2005-04-23", st.ModTime().UTC().Format("2006-01-02"))
	}
}

func TestFormatTrackNote(t *testing.T) {
	cases := []struct {
		name string
		in   client.FormatInfo
		want string
	}{
		{
			name: "audio only",
			in: client.FormatInfo{
				HasAudio: true,
				HasVideo: false,
			},
			want: "audio only",
		},
		{
			name: "video only",
			in: client.FormatInfo{
				HasAudio: false,
				HasVideo: true,
			},
			want: "video only",
		},
		{
			name: "av",
			in: client.FormatInfo{
				HasAudio: true,
				HasVideo: true,
			},
			want: "av",
		},
		{
			name: "none",
			in: client.FormatInfo{
				HasAudio: false,
				HasVideo: false,
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := client.FormatTrackNote(tc.in)
			if got != tc.want {
				t.Fatalf("client.FormatTrackNote()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrintSubtitleTracks(t *testing.T) {
	var buf bytes.Buffer
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	printSubtitleTracks(&client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Example",
	}, []client.SubtitleTrack{
		{LanguageCode: "en", Name: "English", Ext: "vtt"},
		{LanguageCode: "ko", Name: "Korean", AutoGenerated: true},
	})
	w.Close()
	os.Stdout = prev
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	got := buf.String()
	for _, want := range []string{
		"Available subtitles for Example [jNQXAC9IVRw]",
		"en|English|vtt|manual",
		"ko|Korean|vtt|auto",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("subtitle listing missing %q in:\n%s", want, got)
		}
	}
}

func TestPrintThumbnailURL(t *testing.T) {
	var buf bytes.Buffer
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	err = printThumbnailURL(&client.VideoInfo{ID: "jNQXAC9IVRw", ThumbnailURL: "https://i.ytimg.com/vi/jNQXAC9IVRw/hqdefault.jpg"})
	w.Close()
	os.Stdout = prev
	if err != nil {
		t.Fatalf("printThumbnailURL() error = %v", err)
	}
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "https://i.ytimg.com/vi/jNQXAC9IVRw/hqdefault.jpg" {
		t.Fatalf("thumbnail output=%q", buf.String())
	}

	if err := printThumbnailURL(&client.VideoInfo{ID: "missing"}); !errors.Is(err, client.ErrUnavailable) {
		t.Fatalf("missing thumbnail error=%v, want ErrUnavailable", err)
	}
}

func TestPrintRequestedMetadata(t *testing.T) {
	var buf bytes.Buffer
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	err = printRequestedMetadata(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "Example",
		Description: "Line one\nLine two",
		DurationSec: 3723,
	}, "", cli.Options{
		GetTitle:         true,
		GetID:            true,
		GetDescription:   true,
		GetDuration:      true,
		GetMetadataOrder: []string{"id", "title", "duration", "description"},
	}, nil)
	if err != nil {
		t.Fatalf("printRequestedMetadata() error = %v", err)
	}
	w.Close()
	os.Stdout = prev
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	want := "jNQXAC9IVRw\nExample\n1:02:03\nLine one\nLine two\n"
	if buf.String() != want {
		t.Fatalf("metadata output=%q, want %q", buf.String(), want)
	}
}

func TestPrintRequestedMetadata_GetFormat(t *testing.T) {
	var buf bytes.Buffer
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	err = printRequestedMetadata(&client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Example",
		Formats: []client.FormatInfo{
			{Itag: 137, MimeType: "video/mp4", Width: 1920, Height: 1080, Bitrate: 4000000, HasVideo: true},
			{Itag: 140, MimeType: "audio/mp4", Bitrate: 128000, HasAudio: true},
		},
	}, "", cli.Options{
		FormatSelector:   "137",
		GetFormat:        true,
		GetMetadataOrder: []string{"format"},
	}, nil)
	w.Close()
	os.Stdout = prev
	if err != nil {
		t.Fatalf("printRequestedMetadata() error = %v", err)
	}
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "137 - mp4 1920x1080 video only" {
		t.Fatalf("format output=%q", buf.String())
	}
}

func TestPrintRequestedMetadata_GetURLMerged(t *testing.T) {
	var buf bytes.Buffer
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	err = printRequestedMetadata(&client.VideoInfo{
		ID: "jNQXAC9IVRw",
		Formats: []client.FormatInfo{
			{Itag: 137, MimeType: "video/mp4", Width: 1920, Height: 1080, Bitrate: 4000000, HasVideo: true, Ciphered: true},
			{Itag: 140, MimeType: "audio/mp4", Bitrate: 128000, HasAudio: true, Ciphered: true},
		},
	}, "", cli.Options{
		FormatSelector:   "bestvideo+bestaudio",
		GetURL:           true,
		GetMetadataOrder: []string{"url"},
	}, func(itag int) (string, error) {
		return fmt.Sprintf("https://media.example/%d", itag), nil
	})
	w.Close()
	os.Stdout = prev
	if err != nil {
		t.Fatalf("printRequestedMetadata() error = %v", err)
	}
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	want := "https://media.example/137\nhttps://media.example/140\n"
	if buf.String() != want {
		t.Fatalf("url output=%q, want %q", buf.String(), want)
	}
}

func TestPrintRequestedMetadata_PrintFieldsAndTemplate(t *testing.T) {
	var buf bytes.Buffer
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	err = printRequestedMetadata(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "A/B",
		Author:      "Uploader",
		ChannelID:   "UC:123",
		Description: "Description",
		DurationSec: 75,
		UploadDate:  "2005-04-23",
		PublishDate: "2005-04-24",
		Formats: []client.FormatInfo{
			{Itag: 18, URL: "https://media.example/18.mp4", MimeType: `video/mp4; codecs="avc1.42001E, mp4a.40.2"`, Protocol: "https", HasAudio: true, HasVideo: true, Width: 640, Height: 360, FPS: 30, Bitrate: 800000},
		},
	}, "https://www.youtube.com/watch?v=jNQXAC9IVRw", cli.Options{
		FormatSelector:   "18",
		GetMetadataOrder: []string{"print:title", "print:webpage_url", "print:%(id)s:%(title)s:%(format_id)s:%(protocol)s:%(vcodec)s:%(acodec)s:%(resolution)s:%(width)s:%(height)s:%(fps)s:%(tbr)s:%(vbr)s:%(abr)s:%(uploader_id)s:%(channel)s:%(channel_id)s:%(ext)s:%(duration)s:%(upload_date)s:%(release_date)s:%(timestamp)s:%(webpage_url)s:%(original_url)s", "print:url"},
	}, nil)
	w.Close()
	os.Stdout = prev
	if err != nil {
		t.Fatalf("printRequestedMetadata() error = %v", err)
	}
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	want := "A/B\nhttps://www.youtube.com/watch?v=jNQXAC9IVRw\njNQXAC9IVRw:A_B:18:https:avc1.42001E:mp4a.40.2:640x360:640:360:30:800:800:800:" +
		"UC_123:Uploader:UC_123:mp4:1:15:20050423:20050424:20050423000000:https://www.youtube.com/watch?v=jNQXAC9IVRw:https://www.youtube.com/watch?v=jNQXAC9IVRw\nhttps://media.example/18.mp4\n"
	if buf.String() != want {
		t.Fatalf("print output=%q, want %q", buf.String(), want)
	}
}

func TestPrintRequestedMetadata_PrintToFileAppends(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "%(id)s.txt")
	err := printRequestedMetadata(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "Example",
		DurationSec: 75,
		Formats: []client.FormatInfo{
			{Itag: 18, URL: "https://media.example/18.mp4", MimeType: "video/mp4", HasAudio: true, HasVideo: true},
		},
	}, "https://www.youtube.com/watch?v=jNQXAC9IVRw", cli.Options{
		FormatSelector: "18",
		GetMetadataOrder: []string{
			"printfile:title\x00" + outputPath,
			"printfile:%(id)s:%(duration)s\x00" + outputPath,
		},
	}, nil)
	if err != nil {
		t.Fatalf("printRequestedMetadata() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "jNQXAC9IVRw.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := "Example\njNQXAC9IVRw:1:15\n"
	if string(got) != want {
		t.Fatalf("print-to-file output=%q, want %q", string(got), want)
	}
}

func TestSelectedStreamURLs_UsesDirectURLWhenAvailable(t *testing.T) {
	info := &client.VideoInfo{
		ID: "jNQXAC9IVRw",
		Formats: []client.FormatInfo{
			{Itag: 18, URL: "https://media.example/direct.mp4", MimeType: "video/mp4", HasAudio: true, HasVideo: true},
		},
	}
	got, err := selectedStreamURLs(info, cli.Options{FormatSelector: "18"}, func(itag int) (string, error) {
		t.Fatalf("resolver should not be called for direct non-ciphered URL, got itag=%d", itag)
		return "", nil
	})
	if err != nil {
		t.Fatalf("selectedStreamURLs() error = %v", err)
	}
	if len(got) != 1 || got[0] != "https://media.example/direct.mp4" {
		t.Fatalf("urls=%v", got)
	}
}

func TestSelectedFormatsForOptions_Selector(t *testing.T) {
	formats := []client.FormatInfo{
		{Itag: 137, MimeType: "video/mp4", Width: 1920, Height: 1080, Bitrate: 4000000, HasVideo: true},
		{Itag: 140, MimeType: "audio/mp4", Bitrate: 128000, HasAudio: true},
	}
	got, err := selectedFormatsForOptions(formats, cli.Options{FormatSelector: "bestvideo+bestaudio"})
	if err != nil {
		t.Fatalf("selectedFormatsForOptions() error = %v", err)
	}
	if len(got) != 2 || got[0].Itag != 137 || got[1].Itag != 140 {
		t.Fatalf("selected formats=%+v, want 137+140", got)
	}
}

func TestPredictedOutputFilename_TemplateAndMerged(t *testing.T) {
	info := &client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "A/B",
		Author:      "Uploader",
		ChannelID:   "UC:123",
		UploadDate:  "2005-04-23",
		PublishDate: "2005-04-24",
		Formats: []client.FormatInfo{
			{Itag: 137, MimeType: `video/mp4; codecs="avc1.640028"`, Protocol: "https", Width: 1920, Height: 1080, FPS: 30, Bitrate: 4000000, HasVideo: true},
			{Itag: 140, MimeType: `audio/mp4; codecs="mp4a.40.2"`, Protocol: "https", Bitrate: 128000, HasAudio: true},
		},
	}
	got, err := predictedOutputFilename(info, cli.Options{
		FormatSelector: "bestvideo+bestaudio",
		OutputTemplate: "%(upload_date)s-%(format_id)s-%(protocol)s-%(vcodec)s-%(acodec)s-%(resolution)s-%(width)sx%(height)s-%(fps)s-%(tbr)s-%(vbr)s-%(abr)s-%(uploader_id)s-%(channel_id)s-%(channel)s-%(title)s-%(itag)s.%(ext)s",
	})
	if err != nil {
		t.Fatalf("predictedOutputFilename() error = %v", err)
	}
	if got != "20050423-137+140-https-avc1.640028-mp4a.40.2-1920x1080-1920x1080-30-4128-4000-128-UC_123-UC_123-Uploader-A_B-137+140.mp4" {
		t.Fatalf("filename=%q, want selected-format-token filename", got)
	}
}

func TestPredictedOutputFilename_MergeOutputFormat(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "A/B",
		Formats: []client.FormatInfo{
			{Itag: 137, MimeType: `video/mp4; codecs="avc1.640028"`, Width: 1920, Height: 1080, HasVideo: true},
			{Itag: 140, MimeType: `audio/mp4; codecs="mp4a.40.2"`, Bitrate: 128000, HasAudio: true},
		},
	}
	got, err := predictedOutputFilename(info, cli.Options{
		FormatSelector:    "bestvideo+bestaudio",
		MergeOutputFormat: "mkv",
		OutputTemplate:    "%(title)s.%(ext)s",
	})
	if err != nil {
		t.Fatalf("predictedOutputFilename() error = %v", err)
	}
	if got != "A_B.mkv" {
		t.Fatalf("filename=%q, want A_B.mkv", got)
	}
	got, err = predictedOutputFilename(info, cli.Options{
		FormatSelector:    "bestvideo+bestaudio",
		MergeOutputFormat: "webm",
	})
	if err != nil {
		t.Fatalf("predictedOutputFilename() default error = %v", err)
	}
	if got != "A_B [jNQXAC9IVRw].webm" {
		t.Fatalf("filename=%q, want A_B [jNQXAC9IVRw].webm", got)
	}
}

func TestPredictedOutputFilename_RemuxVideo(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "A/B",
		Formats: []client.FormatInfo{
			{Itag: 137, MimeType: `video/mp4; codecs="avc1.640028"`, Width: 1920, Height: 1080, HasVideo: true},
			{Itag: 140, MimeType: `audio/mp4; codecs="mp4a.40.2"`, Bitrate: 128000, HasAudio: true},
		},
	}
	got, err := predictedOutputFilename(info, cli.Options{
		FormatSelector: "bestvideo+bestaudio",
		RemuxVideo:     "mkv",
		OutputTemplate: "%(title)s.%(ext)s",
	})
	if err != nil {
		t.Fatalf("predictedOutputFilename() error = %v", err)
	}
	if got != "A_B.mkv" {
		t.Fatalf("filename=%q, want A_B.mkv", got)
	}
}

func TestPredictedOutputFilename_NumericDefault(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Audio title",
		Formats: []client.FormatInfo{
			{Itag: 140, MimeType: "audio/mp4", Bitrate: 128000, HasAudio: true},
		},
	}
	got, err := predictedOutputFilename(info, cli.Options{FormatSelector: "140"})
	if err != nil {
		t.Fatalf("predictedOutputFilename() error = %v", err)
	}
	if got != "Audio title [jNQXAC9IVRw].mp4" {
		t.Fatalf("filename=%q, want Audio title [jNQXAC9IVRw].mp4", got)
	}
}

func TestPredictedOutputFilename_IDShortcut(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Ignored",
		Formats: []client.FormatInfo{
			{Itag: 18, MimeType: "video/mp4", HasAudio: true, HasVideo: true},
		},
	}
	got, err := predictedOutputFilename(info, cli.Options{OutputUseID: true})
	if err != nil {
		t.Fatalf("predictedOutputFilename() error = %v", err)
	}
	if got != "jNQXAC9IVRw.mp4" {
		t.Fatalf("filename=%q, want jNQXAC9IVRw.mp4", got)
	}
}

func TestPredictedOutputFilename_RestrictFilenames(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "A title / 한국어?",
		Formats: []client.FormatInfo{
			{Itag: 18, MimeType: "video/mp4", HasAudio: true, HasVideo: true},
		},
	}
	got, err := predictedOutputFilename(info, cli.Options{
		OutputTemplate:    "%(title)s.%(ext)s",
		RestrictFilenames: true,
	})
	if err != nil {
		t.Fatalf("predictedOutputFilename() error = %v", err)
	}
	if got != "A_title.mp4" {
		t.Fatalf("filename=%q, want A_title.mp4", got)
	}
}

func TestPredictedOutputFilename_TrimFilenames(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "VeryLongTitle",
		Formats: []client.FormatInfo{
			{Itag: 18, MimeType: "video/mp4", HasAudio: true, HasVideo: true},
		},
	}
	got, err := predictedOutputFilename(info, cli.Options{
		OutputTemplate: "%(title)s.%(ext)s",
		TrimFilenames:  8,
	})
	if err != nil {
		t.Fatalf("predictedOutputFilename() error = %v", err)
	}
	if got != "VeryLong.mp4" {
		t.Fatalf("filename=%q, want VeryLong.mp4", got)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{
		0:    "0:00",
		65:   "1:05",
		3600: "1:00:00",
		3723: "1:02:03",
	}
	for in, want := range cases {
		if got := client.FormatDuration(in); got != want {
			t.Fatalf("client.FormatDuration(%d)=%q, want %q", in, got, want)
		}
	}
}

func TestSubtitleLanguagesFromTracks(t *testing.T) {
	tracks := []client.SubtitleTrack{
		{LanguageCode: "en", Name: "English"},
		{LanguageCode: "ko", Name: "Korean"},
		{LanguageCode: "en", Name: "English auto", AutoGenerated: true},
		{LanguageCode: "ja", Name: "Japanese auto", AutoGenerated: true},
	}

	gotManual := subtitleLanguagesFromTracks(tracks, cli.Options{WriteSubs: true})
	if strings.Join(gotManual, ",") != "en,ko" {
		t.Fatalf("manual langs=%v, want en,ko", gotManual)
	}

	gotAuto := subtitleLanguagesFromTracks(tracks, cli.Options{WriteAutoSubs: true})
	if strings.Join(gotAuto, ",") != "en,ja" {
		t.Fatalf("auto langs=%v, want en,ja", gotAuto)
	}

	gotBoth := subtitleLanguagesFromTracks(tracks, cli.Options{WriteSubs: true, WriteAutoSubs: true})
	if strings.Join(gotBoth, ",") != "en,ko,ja" {
		t.Fatalf("both langs=%v, want en,ko,ja", gotBoth)
	}

	gotAllManual := subtitleLanguagesFromTracksForRequest(tracks, cli.Options{WriteSubs: true}, []string{"all"})
	if strings.Join(gotAllManual, ",") != "en,ko" {
		t.Fatalf("all manual langs=%v, want en,ko", gotAllManual)
	}

	tracks = append(tracks, client.SubtitleTrack{LanguageCode: "live_chat", Name: "Live chat", AutoGenerated: true})
	gotAllAuto := subtitleLanguagesFromTracksForRequest(tracks, cli.Options{WriteAutoSubs: true}, []string{"all", "-live_chat"})
	if strings.Join(gotAllAuto, ",") != "en,ja" {
		t.Fatalf("all auto langs=%v, want en,ja", gotAllAuto)
	}
	if !subtitleLanguagesRequestAll([]string{"ko", "all", "-live_chat"}) {
		t.Fatalf("expected all subtitle language request")
	}
}

func TestBuildDownloadOptions_NumericItag(t *testing.T) {
	got := buildDownloadOptions(cli.Options{
		FormatSelector: "251",
	})
	if got.Itag != 251 {
		t.Fatalf("Itag = %d, want 251", got.Itag)
	}
	if got.FormatSelector != "" {
		t.Fatalf("FormatSelector = %q, want empty", got.FormatSelector)
	}
}

func TestBuildDownloadOptions_MP3Mode(t *testing.T) {
	got := buildDownloadOptions(cli.Options{
		FormatSelector: "mp3",
	})
	if got.Mode != client.SelectionModeMP3 {
		t.Fatalf("Mode = %q, want %q", got.Mode, client.SelectionModeMP3)
	}
	if got.FormatSelector != "" {
		t.Fatalf("FormatSelector = %q, want empty", got.FormatSelector)
	}
}

func TestBuildDownloadOptions_ExtractAudio(t *testing.T) {
	got := buildDownloadOptions(cli.Options{ExtractAudio: true, AudioFormat: "best"})
	if got.Mode != client.SelectionModeAudioOnly {
		t.Fatalf("Mode = %q, want %q", got.Mode, client.SelectionModeAudioOnly)
	}
	got = buildDownloadOptions(cli.Options{ExtractAudio: true, AudioFormat: "mp3", AudioQuality: "192K"})
	if got.Mode != client.SelectionModeMP3 {
		t.Fatalf("Mode = %q, want %q", got.Mode, client.SelectionModeMP3)
	}
	if got.AudioQuality != "192K" {
		t.Fatalf("AudioQuality = %q, want 192K", got.AudioQuality)
	}
	got = buildDownloadOptions(cli.Options{ExtractAudio: true, AudioFormat: "mp3", FormatSelector: "18"})
	if got.Itag != 18 || got.Mode != client.SelectionModeBest {
		t.Fatalf("explicit format should win, got Itag=%d Mode=%q", got.Itag, got.Mode)
	}
}

func TestBuildDownloadOptions_EmbedMetadata(t *testing.T) {
	got := buildDownloadOptions(cli.Options{})
	if !got.NoEmbedMetadata {
		t.Fatalf("NoEmbedMetadata = %v, want true for CLI default", got.NoEmbedMetadata)
	}
	got = buildDownloadOptions(cli.Options{EmbedMetadata: true})
	if got.NoEmbedMetadata {
		t.Fatalf("NoEmbedMetadata = %v, want false when embedding requested", got.NoEmbedMetadata)
	}
}

func TestBuildDownloadOptions_MergeOutputFormat(t *testing.T) {
	got := buildDownloadOptions(cli.Options{MergeOutputFormat: "mkv"})
	if got.MergeOutputFormat != "mkv" {
		t.Fatalf("MergeOutputFormat = %q, want mkv", got.MergeOutputFormat)
	}
	got = buildDownloadOptions(cli.Options{MergeOutputFormat: "../bad"})
	if got.MergeOutputFormat != "mp4" {
		t.Fatalf("MergeOutputFormat = %q, want mp4 fallback", got.MergeOutputFormat)
	}
}

func TestBuildDownloadOptions_RemuxVideo(t *testing.T) {
	got := buildDownloadOptions(cli.Options{RemuxVideo: "mkv"})
	if got.MergeOutputFormat != "mkv" {
		t.Fatalf("MergeOutputFormat = %q, want mkv", got.MergeOutputFormat)
	}
	got = buildDownloadOptions(cli.Options{RemuxVideo: "aac>m4a/mov>mp4/mkv"})
	if got.MergeOutputFormat != "m4a" {
		t.Fatalf("MergeOutputFormat = %q, want m4a", got.MergeOutputFormat)
	}
	got = buildDownloadOptions(cli.Options{RemuxVideo: "mkv", MergeOutputFormat: "webm"})
	if got.MergeOutputFormat != "webm" {
		t.Fatalf("MergeOutputFormat = %q, want explicit webm", got.MergeOutputFormat)
	}
}

func TestBuildDownloadOptions_KeepVideo(t *testing.T) {
	got := buildDownloadOptions(cli.Options{KeepVideo: true})
	if !got.KeepIntermediateFiles {
		t.Fatalf("KeepIntermediateFiles = %v, want true", got.KeepIntermediateFiles)
	}
}

func TestBuildDownloadOptions_ResumeDefaultEnabled(t *testing.T) {
	got := buildDownloadOptions(cli.Options{})
	if !got.Resume {
		t.Fatalf("Resume = %v, want true", got.Resume)
	}
	if !got.UsePartFiles {
		t.Fatalf("UsePartFiles = %v, want true", got.UsePartFiles)
	}
}

func TestBuildDownloadOptions_NoContinueDisablesResume(t *testing.T) {
	got := buildDownloadOptions(cli.Options{
		NoContinue: true,
	})
	if got.Resume {
		t.Fatalf("Resume = %v, want false", got.Resume)
	}
}

func TestBuildDownloadOptions_NoPartDisablesPartFiles(t *testing.T) {
	got := buildDownloadOptions(cli.Options{NoPart: true})
	if got.UsePartFiles {
		t.Fatalf("UsePartFiles = %v, want false", got.UsePartFiles)
	}
}

func TestProcessInputs_AbortOnErrorStopsEarly(t *testing.T) {
	calls := 0
	hadErr := processInputs(context.Background(), nil, []string{"a", "b", "c"}, cli.Options{
		AbortOnError: true,
	}, func(_ context.Context, _ *client.Client, _ string, _ cli.Options) error {
		calls++
		return errors.New("boom")
	})
	if !hadErr {
		t.Fatalf("hadErr = %v, want true", hadErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestProcessInputs_ContinueOnErrorProcessesAll(t *testing.T) {
	calls := 0
	hadErr := processInputs(context.Background(), nil, []string{"a", "b", "c"}, cli.Options{
		AbortOnError: false,
	}, func(_ context.Context, _ *client.Client, _ string, _ cli.Options) error {
		calls++
		return errors.New("boom")
	})
	if !hadErr {
		t.Fatalf("hadErr = %v, want true", hadErr)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestProcessInputsWithExitCode_SelectsHighestCode(t *testing.T) {
	idx := 0
	errs := []error{
		client.ErrInvalidInput,
		client.ErrNoPlayableFormats,
	}
	code := processInputsWithExitCode(context.Background(), nil, []string{"a", "b"}, cli.Options{
		AbortOnError: false,
	}, func(_ context.Context, _ *client.Client, _ string, _ cli.Options) error {
		e := errs[idx]
		idx++
		return e
	})
	if code != exitCodeNoPlayableFormats {
		t.Fatalf("exit code=%d, want %d", code, exitCodeNoPlayableFormats)
	}
}

func TestProcessInputsWithExitCode_BreakOnExistingStopsCleanly(t *testing.T) {
	calls := 0
	code := processInputsWithExitCode(context.Background(), nil, []string{"a", "b"}, cli.Options{
		BreakOnExisting: true,
	}, func(_ context.Context, _ *client.Client, _ string, _ cli.Options) error {
		calls++
		return errBreakOnExisting
	})
	if code != exitCodeSuccess {
		t.Fatalf("exit code=%d, want success", code)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestProcessInputsWithExitCode_MaxDownloadsStopsCleanly(t *testing.T) {
	calls := 0
	code := processInputsWithExitCode(context.Background(), nil, []string{"a", "b"}, cli.Options{
		MaxDownloads: 1,
	}, func(_ context.Context, _ *client.Client, _ string, _ cli.Options) error {
		calls++
		return errMaxDownloadsReached
	})
	if code != exitCodeSuccess {
		t.Fatalf("exit code=%d, want success", code)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestRunPlaylistItems_ContinueOnError(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A"},
		{VideoID: "b", Title: "B"},
		{VideoID: "c", Title: "C"},
	}
	calls := 0
	summary, failures := runPlaylistItems(context.Background(), nil, items, playlistTemplateContext{}, cli.Options{}, func(_ context.Context, _ *client.Client, id string, _ cli.Options) error {
		calls++
		if id == "b" {
			return errors.New("fail-b")
		}
		return nil
	})

	if summary.Total != 3 || summary.Succeeded != 2 || summary.Failed != 1 || summary.Aborted {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if len(failures) != 1 || failures[0].VideoID != "b" {
		t.Fatalf("unexpected failures: %+v", failures)
	}
}

func TestRunPlaylistItems_MaxDownloadsStopsCleanly(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A"},
		{VideoID: "b", Title: "B"},
		{VideoID: "c", Title: "C"},
	}
	calls := 0
	summary, failures := runPlaylistItems(context.Background(), nil, items, playlistTemplateContext{}, cli.Options{
		MaxDownloads: 1,
	}, func(_ context.Context, _ *client.Client, _ string, _ cli.Options) error {
		calls++
		return errMaxDownloadsReached
	})

	if summary.Total != 3 || summary.Succeeded != 1 || summary.Failed != 0 || summary.Skipped != 2 || !summary.Aborted {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if len(failures) != 0 {
		t.Fatalf("failures=%+v, want none", failures)
	}
}

func TestRunPlaylistItems_BreakOnExistingStopsCleanly(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A"},
		{VideoID: "b", Title: "B"},
		{VideoID: "c", Title: "C"},
	}
	calls := 0
	summary, failures := runPlaylistItems(context.Background(), nil, items, playlistTemplateContext{}, cli.Options{
		BreakOnExisting: true,
	}, func(_ context.Context, _ *client.Client, _ string, _ cli.Options) error {
		calls++
		return errBreakOnExisting
	})

	if summary.Total != 3 || summary.Succeeded != 0 || summary.Failed != 0 || summary.Skipped != 2 || !summary.Aborted {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if len(failures) != 0 {
		t.Fatalf("failures=%+v, want none", failures)
	}
}

func TestRunPlaylistItems_AbortOnError(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A"},
		{VideoID: "b", Title: "B"},
		{VideoID: "c", Title: "C"},
	}
	calls := 0
	summary, failures := runPlaylistItems(context.Background(), nil, items, playlistTemplateContext{}, cli.Options{
		AbortOnError: true,
	}, func(_ context.Context, _ *client.Client, id string, _ cli.Options) error {
		calls++
		if id == "b" {
			return errors.New("fail-b")
		}
		return nil
	})

	if summary.Total != 3 || summary.Succeeded != 1 || summary.Failed != 1 || !summary.Aborted {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(failures) != 1 || failures[0].VideoID != "b" {
		t.Fatalf("unexpected failures: %+v", failures)
	}
}

func TestRunPlaylistItems_SkipAfterErrorThreshold(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A"},
		{VideoID: "b", Title: "B"},
		{VideoID: "c", Title: "C"},
		{VideoID: "d", Title: "D"},
	}
	calls := 0
	summary, failures := runPlaylistItems(context.Background(), nil, items, playlistTemplateContext{}, cli.Options{
		SkipPlaylistAfterErrors: 2,
	}, func(_ context.Context, _ *client.Client, id string, _ cli.Options) error {
		calls++
		if id == "b" || id == "c" {
			return errors.New("fail")
		}
		return nil
	})

	if summary.Total != 4 || summary.Succeeded != 1 || summary.Failed != 2 || summary.Skipped != 1 || !summary.Aborted {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
	if len(failures) != 2 {
		t.Fatalf("failures=%d, want 2", len(failures))
	}
}

func TestRunPlaylistItems_AppliesPlaylistTemplateContext(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A", PlaylistIndex: 7},
		{VideoID: "b", Title: "B", PlaylistIndex: 9},
	}
	var gotTemplates []string
	summary, failures := runPlaylistItems(context.Background(), nil, items, playlistTemplateContext{
		ID:         "PL/123",
		Title:      "My: Playlist",
		Uploader:   "Owner/Name",
		UploaderID: "@owner:name",
		Channel:    "Channel/Name",
		ChannelID:  "UC:123",
		Count:      2,
	}, cli.Options{
		OutputTemplate: "%(playlist_id)s/%(playlist_title)s/%(playlist_uploader)s/%(playlist_uploader_id)s/%(playlist_channel)s/%(playlist_channel_id)s/%(playlist_index)s-of-%(playlist_count)s-%(playlist_autonumber)s-%(title)s.%(ext)s",
	}, func(_ context.Context, _ *client.Client, _ string, opts cli.Options) error {
		gotTemplates = append(gotTemplates, opts.OutputTemplate)
		return nil
	})
	if summary.Succeeded != 2 || len(failures) != 0 {
		t.Fatalf("unexpected summary=%+v failures=%v", summary, failures)
	}
	want := []string{
		"PL_123/My_ Playlist/Owner_Name/@owner_name/Channel_Name/UC_123/00007-of-00002-00001-%(title)s.%(ext)s",
		"PL_123/My_ Playlist/Owner_Name/@owner_name/Channel_Name/UC_123/00009-of-00002-00002-%(title)s.%(ext)s",
	}
	if strings.Join(gotTemplates, "|") != strings.Join(want, "|") {
		t.Fatalf("templates=%v, want %v", gotTemplates, want)
	}
}

func TestWithPlaylistIndexesFillsMissingOnly(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a"},
		{VideoID: "b", PlaylistIndex: 10},
	}
	got := withPlaylistIndexes(items)
	if got[0].PlaylistIndex != 1 || got[1].PlaylistIndex != 10 {
		t.Fatalf("indexes=%d,%d want 1,10", got[0].PlaylistIndex, got[1].PlaylistIndex)
	}
	if items[0].PlaylistIndex != 0 {
		t.Fatalf("withPlaylistIndexes must not mutate input")
	}
}

func TestReachedPlaylistFailureThreshold(t *testing.T) {
	if reachedPlaylistFailureThreshold(1, 0) {
		t.Fatalf("threshold 0 should be disabled")
	}
	if reachedPlaylistFailureThreshold(1, -1) {
		t.Fatalf("negative threshold should be disabled")
	}
	if !reachedPlaylistFailureThreshold(2, 2) {
		t.Fatalf("expected threshold hit")
	}
}

func TestFilterPlaylistItems_IndexesAndRanges(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
		{VideoID: "five"},
	}
	got, err := client.SelectPlaylistItems(items, "1,3:4,10")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "one,three,four" {
		t.Fatalf("ids=%v, want one,three,four", ids)
	}
}

func TestFilterPlaylistItems_OpenRanges(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
	}
	got, err := client.SelectPlaylistItems(items, ":2,4:")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "one,two,four" {
		t.Fatalf("ids=%v, want one,two,four", ids)
	}
}

func TestFilterPlaylistItems_HyphenRange(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
	}
	got, err := client.SelectPlaylistItems(items, "2-4")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "two,three,four" {
		t.Fatalf("ids=%v, want two,three,four", ids)
	}
}

func TestFilterPlaylistItems_NegativeIndices(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
		{VideoID: "five"},
	}
	got, err := client.SelectPlaylistItems(items, "-3:-1")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "three,four,five" {
		t.Fatalf("ids=%v, want three,four,five", ids)
	}

	got, err = client.SelectPlaylistItems(items, "-1")
	if err != nil {
		t.Fatalf("filterPlaylistItems() single negative error = %v", err)
	}
	if len(got) != 1 || got[0].VideoID != "five" {
		t.Fatalf("single negative selection=%v, want five", got)
	}

	got, err = client.SelectPlaylistItems(items, "-10")
	if err != nil {
		t.Fatalf("filterPlaylistItems() out-of-range negative error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("out-of-range negative len=%d, want 0", len(got))
	}
}

func TestFilterPlaylistItems_PositiveStep(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
		{VideoID: "five"},
	}
	got, err := client.SelectPlaylistItems(items, "1:5:2")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "one,three,five" {
		t.Fatalf("ids=%v, want one,three,five", ids)
	}
}

func TestFilterPlaylistItems_NegativeStep(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
		{VideoID: "five"},
	}
	got, err := client.SelectPlaylistItems(items, "5:1:-2")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "five,three,one" {
		t.Fatalf("ids=%v, want five,three,one", ids)
	}
}

func TestFilterPlaylistItems_NegativeStepOpenRange(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
	}
	got, err := client.SelectPlaylistItems(items, "::-1")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "three,two,one" {
		t.Fatalf("ids=%v, want three,two,one", ids)
	}
}

func TestFilterPlaylistItems_PreservesRequestedOrderAndDedupes(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
	}
	got, err := client.SelectPlaylistItems(items, "3,1:3")
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "three,one,two" {
		t.Fatalf("ids=%v, want three,one,two", ids)
	}
}

func TestFilterPlaylistItems_InvalidSelector(t *testing.T) {
	_, err := client.SelectPlaylistItems([]client.PlaylistItem{{VideoID: "one"}}, "0")
	if !errors.Is(err, client.ErrInvalidInput) {
		t.Fatalf("error=%v, want ErrInvalidInput", err)
	}
	_, err = client.SelectPlaylistItems([]client.PlaylistItem{{VideoID: "one"}}, "3:1")
	if !errors.Is(err, client.ErrInvalidInput) {
		t.Fatalf("error=%v, want ErrInvalidInput", err)
	}
	_, err = client.SelectPlaylistItems([]client.PlaylistItem{{VideoID: "one"}}, "3-1")
	if !errors.Is(err, client.ErrInvalidInput) {
		t.Fatalf("error=%v, want ErrInvalidInput", err)
	}
	_, err = client.SelectPlaylistItems([]client.PlaylistItem{{VideoID: "one"}}, "1:3:0")
	if !errors.Is(err, client.ErrInvalidInput) {
		t.Fatalf("error=%v, want ErrInvalidInput", err)
	}
	_, err = client.SelectPlaylistItems([]client.PlaylistItem{{VideoID: "one"}}, "1:3:-1")
	if !errors.Is(err, client.ErrInvalidInput) {
		t.Fatalf("error=%v, want ErrInvalidInput", err)
	}
}

func TestPlaylistSelector_StartEnd(t *testing.T) {
	got := playlistSelector(cli.Options{PlaylistStart: 2, PlaylistEnd: 4})
	if got != "2:4" {
		t.Fatalf("playlistSelector()=%q, want 2:4", got)
	}
	filtered, err := client.SelectPlaylistItems([]client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
	}, got)
	if err != nil {
		t.Fatalf("filterPlaylistItems() error = %v", err)
	}
	ids := make([]string, 0, len(filtered))
	for _, item := range filtered {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "two,three,four" {
		t.Fatalf("ids=%v, want two,three,four", ids)
	}
}

func TestPlaylistSelector_ItemsWinsOverStartEnd(t *testing.T) {
	got := playlistSelector(cli.Options{
		PlaylistItems: "1,3",
		PlaylistStart: 2,
		PlaylistEnd:   4,
	})
	if got != "1,3" {
		t.Fatalf("playlistSelector()=%q, want 1,3", got)
	}
}

func TestOrderPlaylistItems_Reverse(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
	}
	got := orderPlaylistItems(items, cli.Options{PlaylistReverse: true})
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "three,two,one" {
		t.Fatalf("ids=%v, want three,two,one", ids)
	}
	if items[0].VideoID != "one" {
		t.Fatalf("orderPlaylistItems must not mutate input slice")
	}
}

func TestOrderPlaylistItems_Random(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
		{VideoID: "four"},
	}
	got := shufflePlaylistItems(items, rand.New(rand.NewSource(1)))
	if len(got) != len(items) {
		t.Fatalf("len=%d, want %d", len(got), len(items))
	}
	seen := map[string]bool{}
	for _, item := range got {
		seen[item.VideoID] = true
	}
	for _, item := range items {
		if !seen[item.VideoID] {
			t.Fatalf("missing item %s after shuffle: %v", item.VideoID, got)
		}
	}
	if got[0].VideoID == "one" && got[1].VideoID == "two" && got[2].VideoID == "three" && got[3].VideoID == "four" {
		t.Fatalf("expected deterministic seeded shuffle to change order")
	}
	if items[0].VideoID != "one" {
		t.Fatalf("shufflePlaylistItems must not mutate input slice")
	}
}

func TestOrderPlaylistItems_ReverseWinsOverRandom(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "one"},
		{VideoID: "two"},
		{VideoID: "three"},
	}
	got := orderPlaylistItems(items, cli.Options{PlaylistReverse: true, PlaylistRandom: true})
	ids := make([]string, 0, len(got))
	for _, item := range got {
		ids = append(ids, item.VideoID)
	}
	if strings.Join(ids, ",") != "three,two,one" {
		t.Fatalf("ids=%v, want three,two,one", ids)
	}
}

func TestParseSubtitleLanguages(t *testing.T) {
	got := parseSubtitleLanguages("ko, en,ko,  ")
	if len(got) != 2 || got[0] != "ko" || got[1] != "en" {
		t.Fatalf("languages=%v, want [ko en]", got)
	}
}

func TestApplySubtitleLanguageExclusions(t *testing.T) {
	got := client.ApplySubtitleLanguageExclusions(parseSubtitleLanguages("en,ko,-ko,en"))
	if strings.Join(got, ",") != "en" {
		t.Fatalf("languages=%v, want [en]", got)
	}
	got = client.ApplySubtitleLanguageExclusions(parseSubtitleLanguages("-ko"))
	if len(got) != 0 {
		t.Fatalf("languages=%v, want empty after exclusion-only request", got)
	}
}

func TestSubtitleOutputPath_Default(t *testing.T) {
	path := subtitleOutputPath("", &client.VideoInfo{
		ID: "abc123",
	}, "ko", "srt")
	if path != "abc123.ko.srt" {
		t.Fatalf("path=%q, want %q", path, "abc123.ko.srt")
	}
}

func TestSubtitleOutputPath_Template(t *testing.T) {
	path := subtitleOutputPath("%(title)s.%(ext)s", &client.VideoInfo{
		ID:     "abc123",
		Title:  "title/name",
		Author: "owner",
	}, "en", "srt")
	if path != "title_name.en.srt" {
		t.Fatalf("path=%q, want %q", path, "title_name.en.srt")
	}
}

func TestSubtitleOutputPath_TemplateVTT(t *testing.T) {
	path := subtitleOutputPath("%(title)s.%(ext)s", &client.VideoInfo{
		ID:     "abc123",
		Title:  "title/name",
		Author: "owner",
	}, "en", "vtt")
	if path != "title_name.en.vtt" {
		t.Fatalf("path=%q, want %q", path, "title_name.en.vtt")
	}
}

func TestInfoJSONOutputPath_Default(t *testing.T) {
	path := infoJSONOutputPath("", &client.VideoInfo{
		ID: "jNQXAC9IVRw",
	})
	if path != "jNQXAC9IVRw.info.json" {
		t.Fatalf("path=%q, want jNQXAC9IVRw.info.json", path)
	}
}

func TestInfoJSONOutputPath_Template(t *testing.T) {
	path := infoJSONOutputPath(filepath.Join(t.TempDir(), "%(title)s.%(ext)s"), &client.VideoInfo{
		ID:     "abc123",
		Title:  "title/name",
		Author: "owner",
	})
	if !strings.HasSuffix(path, filepath.Join("title_name.info.json")) {
		t.Fatalf("path=%q, want suffix title_name.info.json", path)
	}
}

func TestWriteInfoJSONSidecar(t *testing.T) {
	dir := t.TempDir()
	info := &client.VideoInfo{
		ID:     "jNQXAC9IVRw",
		Title:  "Me at the zoo",
		Author: "jawed",
		Formats: []client.FormatInfo{
			{
				Itag:     18,
				URL:      "https://cdn.example/av.mp4",
				MimeType: "video/mp4",
				HasAudio: true,
				HasVideo: true,
				Width:    640,
				Height:   360,
				Bitrate:  800000,
			},
		},
	}
	err := writeInfoJSONSidecar("https://youtu.be/jNQXAC9IVRw", info, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
	})
	if err != nil {
		t.Fatalf("writeInfoJSONSidecar() error = %v", err)
	}
	out := filepath.Join(dir, "Me at the zoo.info.json")
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read info json: %v", err)
	}
	var payload ytdlpDumpSingleJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.ID != "jNQXAC9IVRw" || payload.ExtractorKey != "Youtube" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.URL != "https://cdn.example/av.mp4" {
		t.Fatalf("payload.URL=%q, want playable URL", payload.URL)
	}
}

func TestWritePlaylistInfoJSONSidecar(t *testing.T) {
	dir := t.TempDir()
	playlist := &client.PlaylistInfo{
		ID:         "PL1234567890",
		Title:      "My Playlist",
		Uploader:   "Owner",
		UploaderID: "@owner",
		Channel:    "Owner Channel",
		ChannelID:  "UC123",
		Items: []client.PlaylistItem{
			{VideoID: "aaaaaaaaaaa", Title: "one", Author: "author1", DurationSec: 60, PlaylistIndex: 3},
			{VideoID: "bbbbbbbbbbb", Title: "two", Author: "author2", DurationSec: 120},
		},
	}
	err := writePlaylistInfoJSONSidecar(playlist, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
	})
	if err != nil {
		t.Fatalf("writePlaylistInfoJSONSidecar() error = %v", err)
	}
	out := filepath.Join(dir, "My Playlist.info.json")
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read playlist info json: %v", err)
	}
	var payload ytdlpPlaylistInfoJSON
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.ID != "PL1234567890" || payload.ExtractorKey != "YoutubePlaylist" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.WebpageURL != "https://www.youtube.com/playlist?list=PL1234567890" {
		t.Fatalf("WebpageURL=%q, want canonical playlist URL", payload.WebpageURL)
	}
	if len(payload.Entries) != 2 {
		t.Fatalf("entries=%d, want 2", len(payload.Entries))
	}
	if payload.Entries[0].PlaylistIndex != 3 || payload.Entries[1].PlaylistIndex != 2 {
		t.Fatalf("playlist indexes=%d,%d want 3,2", payload.Entries[0].PlaylistIndex, payload.Entries[1].PlaylistIndex)
	}
}

func TestWritePlaylistInfoJSONSidecar_NoOverwrites(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "My Playlist.info.json")
	if err := os.WriteFile(out, []byte("existing"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	err := writePlaylistInfoJSONSidecar(&client.PlaylistInfo{
		ID:    "PL1234567890",
		Title: "My Playlist",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
		NoOverwrites:   true,
	})
	if err != nil {
		t.Fatalf("writePlaylistInfoJSONSidecar() error = %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(raw) != "existing" {
		t.Fatalf("sidecar was overwritten: %q", string(raw))
	}
}

func TestDescriptionOutputPath_Default(t *testing.T) {
	path := descriptionOutputPath("", &client.VideoInfo{
		ID: "jNQXAC9IVRw",
	})
	if path != "jNQXAC9IVRw.description" {
		t.Fatalf("path=%q, want jNQXAC9IVRw.description", path)
	}
}

func TestDescriptionOutputPath_Template(t *testing.T) {
	path := descriptionOutputPath(filepath.Join(t.TempDir(), "%(title)s.%(ext)s"), &client.VideoInfo{
		ID:     "abc123",
		Title:  "title/name",
		Author: "owner",
	})
	if !strings.HasSuffix(path, filepath.Join("title_name.description")) {
		t.Fatalf("path=%q, want suffix title_name.description", path)
	}
}

func TestURLLinkOutputPath_Template(t *testing.T) {
	path := urlLinkOutputPath(filepath.Join(t.TempDir(), "%(title)s.%(ext)s"), &client.VideoInfo{
		ID:     "abc123",
		Title:  "title/name",
		Author: "owner",
	})
	if !strings.HasSuffix(path, filepath.Join("title_name.url")) {
		t.Fatalf("path=%q, want suffix title_name.url", path)
	}
}

func TestWriteURLLinkSidecar(t *testing.T) {
	dir := t.TempDir()
	err := writeShortcutSidecar("https://youtu.be/jNQXAC9IVRw", &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Me at the zoo",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
		Quiet:          true,
	}, shortcutURL)
	if err != nil {
		t.Fatalf("writeURLLinkSidecar() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "Me at the zoo.url"))
	if err != nil {
		t.Fatalf("read url link: %v", err)
	}
	want := "[InternetShortcut]\r\nURL=https://youtu.be/jNQXAC9IVRw\r\n"
	if string(raw) != want {
		t.Fatalf("url link=%q, want %q", string(raw), want)
	}
}

func TestWriteURLLinkSidecar_NoOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jNQXAC9IVRw.url")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write existing url link: %v", err)
	}
	err := writeShortcutSidecar("https://youtu.be/jNQXAC9IVRw", &client.VideoInfo{ID: "jNQXAC9IVRw"}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(id)s.%(ext)s"),
		NoOverwrites:   true,
		Quiet:          true,
	}, shortcutURL)
	if err != nil {
		t.Fatalf("writeURLLinkSidecar() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read url link: %v", err)
	}
	if string(raw) != "old" {
		t.Fatalf("url link=%q, want old", string(raw))
	}
}

func TestWriteShortcutSidecar_Webloc(t *testing.T) {
	dir := t.TempDir()
	err := writeShortcutSidecar("https://youtu.be/watch?v=1&x=2", &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Me at the zoo",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
		Quiet:          true,
	}, shortcutWebloc)
	if err != nil {
		t.Fatalf("writeShortcutSidecar() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "Me at the zoo.webloc"))
	if err != nil {
		t.Fatalf("read webloc: %v", err)
	}
	if !strings.Contains(string(raw), "<key>URL</key>") || !strings.Contains(string(raw), "watch?v=1&amp;x=2") {
		t.Fatalf("unexpected webloc body: %q", string(raw))
	}
}

func TestWriteShortcutSidecar_Desktop(t *testing.T) {
	dir := t.TempDir()
	err := writeShortcutSidecar("https://youtu.be/jNQXAC9IVRw", &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Me\nZoo",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(id)s.%(ext)s"),
		Quiet:          true,
	}, shortcutDesktop)
	if err != nil {
		t.Fatalf("writeShortcutSidecar() error = %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "jNQXAC9IVRw.desktop"))
	if err != nil {
		t.Fatalf("read desktop: %v", err)
	}
	want := "[Desktop Entry]\nType=Link\nName=Me\\nZoo\nURL=https://youtu.be/jNQXAC9IVRw\n"
	if string(raw) != want {
		t.Fatalf("desktop body=%q, want %q", string(raw), want)
	}
}

func TestWriteDescriptionSidecar(t *testing.T) {
	dir := t.TempDir()
	info := &client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "Me at the zoo",
		Author:      "jawed",
		Description: "first YouTube video",
	}
	err := writeDescriptionSidecar(info, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
	})
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	out := filepath.Join(dir, "Me at the zoo.description")
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read description: %v", err)
	}
	if string(raw) != "first YouTube video" {
		t.Fatalf("description=%q, want original text", string(raw))
	}
}

func TestThumbnailOutputPath_Template(t *testing.T) {
	path := thumbnailOutputPath(filepath.Join(t.TempDir(), "%(title)s.%(ext)s"), &client.VideoInfo{
		ID:           "abc123",
		Title:        "title/name",
		Author:       "owner",
		ThumbnailURL: "https://i.example/thumb.webp?x=1",
	})
	if !strings.HasSuffix(path, filepath.Join("title_name.webp")) {
		t.Fatalf("path=%q, want suffix title_name.webp", path)
	}
}

func TestWriteThumbnailSidecar(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/thumb.jpg" {
			t.Fatalf("unexpected thumbnail request path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	defer server.Close()

	dir := t.TempDir()
	c := client.New(client.Config{HTTPClient: server.Client()})
	info := &client.VideoInfo{
		ID:           "jNQXAC9IVRw",
		Title:        "Me at the zoo",
		Author:       "jawed",
		ThumbnailURL: server.URL + "/thumb.jpg?width=1280",
	}
	err := writeThumbnailSidecar(context.Background(), c, info, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
	})
	if err != nil {
		t.Fatalf("writeThumbnailSidecar() error = %v", err)
	}
	out := filepath.Join(dir, "Me at the zoo.jpg")
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read thumbnail: %v", err)
	}
	if string(raw) != "jpeg-bytes" {
		t.Fatalf("thumbnail bytes=%q", string(raw))
	}
}

func TestResolveSubtitleOutputFormat(t *testing.T) {
	if got := client.ResolveSubtitleOutputFormat("vtt/srt"); got != client.SubtitleOutputFormatVTT {
		t.Fatalf("ResolveSubtitleOutputFormat(vtt/srt)=%q, want %q", got, client.SubtitleOutputFormatVTT)
	}
	if got := client.ResolveSubtitleOutputFormat("best"); got != client.SubtitleOutputFormatSRT {
		t.Fatalf("ResolveSubtitleOutputFormat(best)=%q, want %q", got, client.SubtitleOutputFormatSRT)
	}
	if got := client.ResolveSubtitleOutputFormat("srv3/ttml"); got != client.SubtitleOutputFormatSRT {
		t.Fatalf("ResolveSubtitleOutputFormat(srv3/ttml)=%q, want %q", got, client.SubtitleOutputFormatSRT)
	}
}

func TestWriteTranscriptAsSRT(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "x.ko.srt")
	err := client.WriteTranscript(out, &client.Transcript{
		Entries: []client.TranscriptEntry{
			{StartSec: 0.0, DurSec: 1.5, Text: "hello"},
			{StartSec: 1.5, DurSec: 0.5, Text: "world"},
		},
	}, client.SubtitleOutputFormatSRT)
	if err != nil {
		t.Fatalf("WriteTranscript(SRT) error = %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	txt := string(raw)
	if !strings.Contains(txt, "00:00:00,000 --> 00:00:01,500") {
		t.Fatalf("unexpected srt output: %q", txt)
	}
	if !strings.Contains(txt, "\n2\n00:00:01,500 --> 00:00:02,000\nworld\n") {
		t.Fatalf("unexpected srt output: %q", txt)
	}
}

func TestWriteTranscriptAsVTT(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "x.ko.vtt")
	err := client.WriteTranscript(out, &client.Transcript{
		Entries: []client.TranscriptEntry{
			{StartSec: 0.0, DurSec: 1.5, Text: "hello"},
			{StartSec: 1.5, DurSec: 0.5, Text: "world"},
		},
	}, client.SubtitleOutputFormatVTT)
	if err != nil {
		t.Fatalf("WriteTranscript(VTT) error = %v", err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	txt := string(raw)
	if !strings.Contains(txt, "WEBVTT") {
		t.Fatalf("unexpected vtt output: %q", txt)
	}
	if !strings.Contains(txt, "00:00:00.000 --> 00:00:01.500") {
		t.Fatalf("unexpected vtt output: %q", txt)
	}
}

func TestDownloadArchive_LoadIgnoresCorruptedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.txt")
	content := "jNQXAC9IVRw\nnot-a-video-id\nhttps://example.com/watch?v=bad\nDSYFmhjDbvs\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write seed archive: %v", err)
	}

	archive, err := newDownloadArchive(path)
	if err != nil {
		t.Fatalf("newDownloadArchive() error = %v", err)
	}
	defer archive.Close()

	if !archive.Has("jNQXAC9IVRw") || !archive.Has("DSYFmhjDbvs") {
		t.Fatalf("expected valid IDs to be loaded")
	}
	if archive.Has("not-a-video-id") {
		t.Fatalf("corrupted line must not be loaded")
	}
}

func TestDownloadArchive_AddIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.txt")
	archive, err := newDownloadArchive(path)
	if err != nil {
		t.Fatalf("newDownloadArchive() error = %v", err)
	}
	defer archive.Close()

	if err := archive.Add("jNQXAC9IVRw"); err != nil {
		t.Fatalf("archive.Add() error = %v", err)
	}
	if err := archive.Add("jNQXAC9IVRw"); err != nil {
		t.Fatalf("archive.Add() duplicate error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read archive file: %v", err)
	}
	lines := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines != 1 {
		t.Fatalf("archive line count=%d, want 1", lines)
	}
}

func TestShouldSkipDownloadByArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.txt")
	archive, err := newDownloadArchive(path)
	if err != nil {
		t.Fatalf("newDownloadArchive() error = %v", err)
	}
	defer archive.Close()
	if err := archive.Add("jNQXAC9IVRw"); err != nil {
		t.Fatalf("archive.Add() error = %v", err)
	}

	prev := activeDownloadArchive
	activeDownloadArchive = archive
	defer func() { activeDownloadArchive = prev }()

	if !shouldSkipDownloadByArchive("https://www.youtube.com/watch?v=jNQXAC9IVRw", cli.Options{}) {
		t.Fatalf("expected archive hit skip")
	}
	if shouldSkipDownloadByArchive("https://www.youtube.com/watch?v=DSYFmhjDbvs", cli.Options{}) {
		t.Fatalf("unexpected skip for non-archived id")
	}
}

func TestRecordCompletedDownload_AppendsArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.txt")
	archive, err := newDownloadArchive(path)
	if err != nil {
		t.Fatalf("newDownloadArchive() error = %v", err)
	}
	defer archive.Close()

	prev := activeDownloadArchive
	activeDownloadArchive = archive
	defer func() { activeDownloadArchive = prev }()

	if err := recordCompletedDownload("DSYFmhjDbvs"); err != nil {
		t.Fatalf("recordCompletedDownload() error = %v", err)
	}
	if !archive.Has("DSYFmhjDbvs") {
		t.Fatalf("expected recorded video ID in archive")
	}
}

func TestRecordCompletedDownload_EnforcesMaxDownloads(t *testing.T) {
	prevArchive := activeDownloadArchive
	prevLimit := activeDownloadLimit
	activeDownloadArchive = nil
	activeDownloadLimit = &downloadLimit{Max: 2}
	defer func() {
		activeDownloadArchive = prevArchive
		activeDownloadLimit = prevLimit
	}()

	if err := recordCompletedDownload("jNQXAC9IVRw"); err != nil {
		t.Fatalf("first recordCompletedDownload() error = %v", err)
	}
	if err := recordCompletedDownload("DSYFmhjDbvs"); !errors.Is(err, errMaxDownloadsReached) {
		t.Fatalf("second recordCompletedDownload() error = %v, want max downloads", err)
	}
	if activeDownloadLimit.Count != 2 {
		t.Fatalf("download count=%d, want 2", activeDownloadLimit.Count)
	}
}

func TestRecordForcedArchiveIfRequested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.txt")
	archive, err := newDownloadArchive(path)
	if err != nil {
		t.Fatalf("newDownloadArchive() error = %v", err)
	}
	defer archive.Close()

	prev := activeDownloadArchive
	activeDownloadArchive = archive
	defer func() { activeDownloadArchive = prev }()

	info := &client.VideoInfo{ID: "jNQXAC9IVRw"}
	if err := recordForcedArchiveIfRequested(info, cli.Options{}); err != nil {
		t.Fatalf("recordForcedArchiveIfRequested() disabled error = %v", err)
	}
	if archive.Has("jNQXAC9IVRw") {
		t.Fatalf("archive recorded while ForceWriteArchive disabled")
	}
	if err := recordForcedArchiveIfRequested(info, cli.Options{ForceWriteArchive: true}); err != nil {
		t.Fatalf("recordForcedArchiveIfRequested() error = %v", err)
	}
	if !archive.Has("jNQXAC9IVRw") {
		t.Fatalf("expected forced archive record")
	}
}

func TestWarnf_SuppressedByNoWarnings(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	}()

	warnf(cli.Options{NoWarnings: true}, "this should not print")
	if got := strings.TrimSpace(buf.String()); got != "" {
		t.Fatalf("expected no warning output, got %q", got)
	}
}

func TestWarnf_EmitsWarningByDefault(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	}()

	warnf(cli.Options{}, "subtitle partial failure: %s", "ko(not found)")
	got := strings.TrimSpace(buf.String())
	if !strings.Contains(got, "WARNING: subtitle partial failure: ko(not found)") {
		t.Fatalf("unexpected warning output: %q", got)
	}
}

func TestEmitFlatPlaylist_Text(t *testing.T) {
	var buf bytes.Buffer
	err := emitFlatPlaylist([]client.PlaylistItem{
		{VideoID: "jNQXAC9IVRw", Title: "one"},
		{VideoID: "DSYFmhjDbvs", Title: "two"},
	}, cli.Options{}, &buf)
	if err != nil {
		t.Fatalf("emitFlatPlaylist() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[flat] jNQXAC9IVRw\tone") {
		t.Fatalf("unexpected flat text output: %q", out)
	}
	if !strings.Contains(out, "[flat] DSYFmhjDbvs\ttwo") {
		t.Fatalf("unexpected flat text output: %q", out)
	}
}

func TestEmitFlatPlaylist_JSON(t *testing.T) {
	var buf bytes.Buffer
	err := emitFlatPlaylist([]client.PlaylistItem{
		{VideoID: "jNQXAC9IVRw", Title: "one"},
	}, cli.Options{PrintJSON: true}, &buf)
	if err != nil {
		t.Fatalf("emitFlatPlaylist() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines=%d, want 1 output line", len(lines))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload["_type"] != "url" || payload["id"] != "jNQXAC9IVRw" {
		t.Fatalf("unexpected payload: %v", payload)
	}
	if payload["url"] != "https://www.youtube.com/watch?v=jNQXAC9IVRw" {
		t.Fatalf("unexpected payload url: %v", payload["url"])
	}
}

func TestShouldPrintPlaylistText(t *testing.T) {
	if !shouldPrintPlaylistText(cli.Options{}) {
		t.Fatalf("expected text output for default playlist mode")
	}
	if shouldPrintPlaylistText(cli.Options{PrintJSON: true}) {
		t.Fatalf("expected text output suppression for print-json")
	}
	if shouldPrintPlaylistText(cli.Options{DumpSingleJSON: true}) {
		t.Fatalf("expected text output suppression for dump-single-json")
	}
	if shouldPrintPlaylistText(cli.Options{Quiet: true}) {
		t.Fatalf("expected text output suppression for quiet mode")
	}
	if !shouldPrintHumanText(cli.Options{}) {
		t.Fatalf("expected human text output by default")
	}
	if shouldPrintHumanText(cli.Options{Quiet: true}) {
		t.Fatalf("expected human text output suppression for quiet mode")
	}
	if !shouldPrintProgressText(cli.Options{}) {
		t.Fatalf("expected progress text output by default")
	}
	if shouldPrintProgressText(cli.Options{NoProgress: true}) {
		t.Fatalf("expected progress text output suppression for no-progress")
	}
	if !shouldPrintProgressText(cli.Options{Quiet: true, Progress: true}) {
		t.Fatalf("expected --progress to override quiet mode for progress text")
	}
	if shouldPrintProgressText(cli.Options{PrintJSON: true, Progress: true}) {
		t.Fatalf("expected print-json to suppress progress text")
	}
}

func TestWriteDescriptionSidecar_QuietSuppressesStatus(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	err = writeDescriptionSidecar(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "Example",
		Description: "description",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(id)s.%(ext)s"),
		Quiet:          true,
	})
	w.Close()
	os.Stdout = prev
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}
	if buf.String() != "" {
		t.Fatalf("quiet output=%q, want empty", buf.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "jNQXAC9IVRw.description")); err != nil {
		t.Fatalf("description sidecar not written: %v", err)
	}
}

func TestWriteDescriptionSidecar_OutputPathDir(t *testing.T) {
	dir := t.TempDir()
	err := writeDescriptionSidecar(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "Example",
		Description: "description",
	}, cli.Options{
		OutputPathDir: dir,
		Quiet:         true,
	})
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Example [jNQXAC9IVRw].description")); err != nil {
		t.Fatalf("description sidecar not written under output path dir: %v", err)
	}
}

func TestWriteDescriptionSidecar_OutputIDShortcut(t *testing.T) {
	dir := t.TempDir()
	err := writeDescriptionSidecar(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "Ignored",
		Description: "description",
	}, cli.Options{
		OutputPathDir: dir,
		OutputUseID:   true,
		Quiet:         true,
	})
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "jNQXAC9IVRw.description")); err != nil {
		t.Fatalf("description sidecar not written with id basename: %v", err)
	}
}

func TestWriteDescriptionSidecar_RestrictFilenames(t *testing.T) {
	dir := t.TempDir()
	err := writeDescriptionSidecar(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "A title / 한국어?",
		Description: "description",
	}, cli.Options{
		OutputTemplate:    filepath.Join(dir, "%(title)s.%(ext)s"),
		RestrictFilenames: true,
		Quiet:             true,
	})
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "A_title.description")); err != nil {
		t.Fatalf("restricted description sidecar not written: %v", err)
	}
}

func TestWriteDescriptionSidecar_TrimFilenames(t *testing.T) {
	dir := t.TempDir()
	err := writeDescriptionSidecar(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Title:       "VeryLongTitle",
		Description: "description",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(title)s.%(ext)s"),
		TrimFilenames:  8,
		Quiet:          true,
	})
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "VeryLong.description")); err != nil {
		t.Fatalf("trimmed description sidecar not written: %v", err)
	}
}

func TestWriteDescriptionSidecar_NoOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jNQXAC9IVRw.description")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write existing description: %v", err)
	}
	err := writeDescriptionSidecar(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Description: "new",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(id)s.%(ext)s"),
		NoOverwrites:   true,
		Quiet:          true,
	})
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read description: %v", err)
	}
	if string(raw) != "old" {
		t.Fatalf("description=%q, want old", string(raw))
	}
}

func TestWriteDescriptionSidecar_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jNQXAC9IVRw.description")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatalf("write existing description: %v", err)
	}
	err := writeDescriptionSidecar(&client.VideoInfo{
		ID:          "jNQXAC9IVRw",
		Description: "new",
	}, cli.Options{
		OutputTemplate: filepath.Join(dir, "%(id)s.%(ext)s"),
		NoOverwrites:   false,
		Quiet:          true,
	})
	if err != nil {
		t.Fatalf("writeDescriptionSidecar() error = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read description: %v", err)
	}
	if string(raw) != "new" {
		t.Fatalf("description=%q, want new", string(raw))
	}
}

func TestBuildDumpSingleJSONPayload_IncludesPlayableURL(t *testing.T) {
	info := &client.VideoInfo{
		ID:    "jNQXAC9IVRw",
		Title: "Me at the zoo",
		Formats: []client.FormatInfo{
			{
				Itag:     140,
				URL:      "https://cdn.example/audio.m4a",
				MimeType: "audio/mp4",
				HasAudio: true,
				Bitrate:  128000,
			},
			{
				Itag:     18,
				URL:      "https://cdn.example/av.mp4",
				MimeType: "video/mp4",
				HasAudio: true,
				HasVideo: true,
				Width:    640,
				Height:   360,
				Bitrate:  800000,
			},
		},
	}
	payload := buildDumpSingleJSONPayload("https://www.youtube.com/watch?v=jNQXAC9IVRw", info)
	if payload.URL != "https://cdn.example/av.mp4" {
		t.Fatalf("payload.URL=%q, want av format URL", payload.URL)
	}
	if payload.WebpageURL != "https://www.youtube.com/watch?v=jNQXAC9IVRw" {
		t.Fatalf("payload.WebpageURL=%q", payload.WebpageURL)
	}
	if len(payload.Formats) != 2 {
		t.Fatalf("formats len=%d, want 2", len(payload.Formats))
	}
}
