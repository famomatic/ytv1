package main

import (
	"context"
	"errors"
	"io"
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
	}, "ko")
	if path != "abc123.ko.srt" {
		t.Fatalf("path=%q, want %q", path, "abc123.ko.srt")
	}
}

func TestSubtitleOutputPath_Template(t *testing.T) {
	path := subtitleOutputPath("%(title)s.%(ext)s", &client.VideoInfo{
		ID:     "abc123",
		Title:  "title/name",
		Author: "owner",
	}, "en")
	if path != "title_name.en.srt" {
		t.Fatalf("path=%q, want %q", path, "title_name.en.srt")
	}
}

func TestFormatSRTTimestamp(t *testing.T) {
	got := formatSRTTimestamp(3661.234)
	if got != "01:01:01,234" {
		t.Fatalf("timestamp=%q, want %q", got, "01:01:01,234")
	}
}

func TestWriteTranscriptAsSRT(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub", "x.ko.srt")
	err := writeTranscriptAsSRT(out, &client.Transcript{
		Entries: []client.TranscriptEntry{
			{StartSec: 0.0, DurSec: 1.5, Text: "hello"},
			{StartSec: 1.5, DurSec: 0.5, Text: "world"},
		},
	})
	if err != nil {
		t.Fatalf("writeTranscriptAsSRT() error = %v", err)
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
