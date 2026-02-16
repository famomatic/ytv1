package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
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
			got := formatTrackNote(tc.in)
			if got != tc.want {
				t.Fatalf("formatTrackNote()=%q, want %q", got, tc.want)
			}
		})
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

func TestBuildDownloadOptions_ResumeDefaultEnabled(t *testing.T) {
	got := buildDownloadOptions(cli.Options{})
	if !got.Resume {
		t.Fatalf("Resume = %v, want true", got.Resume)
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

func TestRunPlaylistItems_ContinueOnError(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A"},
		{VideoID: "b", Title: "B"},
		{VideoID: "c", Title: "C"},
	}
	calls := 0
	summary, failures := runPlaylistItems(context.Background(), nil, items, cli.Options{}, func(_ context.Context, _ *client.Client, id string, _ cli.Options) error {
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

func TestRunPlaylistItems_AbortOnError(t *testing.T) {
	items := []client.PlaylistItem{
		{VideoID: "a", Title: "A"},
		{VideoID: "b", Title: "B"},
		{VideoID: "c", Title: "C"},
	}
	calls := 0
	summary, failures := runPlaylistItems(context.Background(), nil, items, cli.Options{
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

func TestParseSubtitleLanguages(t *testing.T) {
	got := parseSubtitleLanguages("ko, en,ko,  ")
	if len(got) != 2 || got[0] != "ko" || got[1] != "en" {
		t.Fatalf("languages=%v, want [ko en]", got)
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

	if !shouldSkipDownloadByArchive("https://www.youtube.com/watch?v=jNQXAC9IVRw") {
		t.Fatalf("expected archive hit skip")
	}
	if shouldSkipDownloadByArchive("https://www.youtube.com/watch?v=DSYFmhjDbvs") {
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
