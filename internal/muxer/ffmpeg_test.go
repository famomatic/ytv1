package muxer

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/famomatic/ytv1/internal/types"
)

func TestFFmpegFailurePreservesPreviousOutput(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(output, []byte("previous"), 0600); err != nil {
		t.Fatal(err)
	}
	m := NewFFmpegMuxer(filepath.Join(dir, "missing-ffmpeg"))
	if err := m.Merge(context.Background(), "video.mp4", "audio.m4a", output, types.Metadata{}); err == nil {
		t.Fatal("expected failure")
	}
	b, err := os.ReadFile(output)
	if err != nil || string(b) != "previous" {
		t.Fatalf("output lost: %q %v", b, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary output leaked: %v %v", entries, err)
	}
}

func TestFFmpegMuxerMergeArgsIncludesExtraArgs(t *testing.T) {
	m := NewFFmpegMuxerWithExtraArgs("ffmpeg", []string{"-movflags", "+faststart"})
	got := m.mergeArgs("video.mp4", "audio.m4a", "out.mp4", types.Metadata{
		Title:  "Title",
		Artist: "Artist",
	})
	want := []string{
		"-protocol_whitelist", "file",
		"-i", "video.mp4",
		"-i", "audio.m4a",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c:v", "copy",
		"-c:a", "copy",
		"-metadata", "title=Title",
		"-metadata", "artist=Artist",
		"-movflags", "+faststart",
		"-y", "out.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeArgs()=%v, want %v", got, want)
	}
}
