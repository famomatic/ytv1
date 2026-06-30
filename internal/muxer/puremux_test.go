package muxer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/famomatic/ytv1/internal/types"
)

// fakeMuxer is a test double recording Merge calls. It is always Available
// unless its available flag is explicitly cleared.
type fakeMuxer struct {
	available bool
	merged    int
	lastOut   string
	err       error
}

func (f *fakeMuxer) Available() bool { return f.available }
func (f *fakeMuxer) Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error {
	f.merged++
	f.lastOut = outputPath
	if f.err != nil {
		return f.err
	}
	return os.WriteFile(outputPath, []byte("fake-merged"), 0o644)
}

func TestPureMuxMuxerAlwaysAvailable(t *testing.T) {
	m := NewPureMuxMuxer(nil)
	if !m.Available() {
		t.Fatal("PureMuxMuxer.Available() = false, want true (pure Go, no binary required)")
	}
}

func TestPureMuxMuxerMergesWebMInputs(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.webm")
	if err := writeMinimalWebM(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}

	m := NewPureMuxMuxer(nil)
	if err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	b, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if len(b) < 4 || b[0] != 0x1A || b[1] != 0x45 || b[2] != 0xDF || b[3] != 0xA3 {
		t.Fatalf("output is not a WebM/Matroska file: % x", b[:min(8, len(b))])
	}

	if _, err := os.Stat(videoPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("video intermediate should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(audioPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("audio intermediate should be removed, stat err=%v", err)
	}
}

func TestPureMuxMuxerFallsBackOnUnsupportedOutput(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.mp4")
	if err := writeMinimalWebM(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}

	fb := &fakeMuxer{available: true}
	m := NewPureMuxMuxer(fb)
	if err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if fb.merged != 1 {
		t.Fatalf("fallback merged %d times, want 1", fb.merged)
	}
	if fb.lastOut != outputPath {
		t.Errorf("fallback output = %q, want %q", fb.lastOut, outputPath)
	}
}

func TestPureMuxMuxerFallsBackOnMetadataRequest(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.webm")
	if err := writeMinimalWebM(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}

	fb := &fakeMuxer{available: true}
	m := NewPureMuxMuxer(fb)
	meta := types.Metadata{Title: "Some Title", Artist: "Some Artist"}
	if err := m.Merge(context.Background(), videoPath, audioPath, outputPath, meta); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if fb.merged != 1 {
		t.Fatalf("fallback merged %d times, want 1 (metadata embeds need ffmpeg)", fb.merged)
	}
}

func TestPureMuxMuxerUnavailableWhenNoFallbackForMP4(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.mp4")
	if err := writeMinimalWebM(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}

	m := NewPureMuxMuxer(nil)
	err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{})
	if !errors.Is(err, ErrPureMuxUnavailable) {
		t.Fatalf("Merge err = %v, want ErrPureMuxUnavailable", err)
	}
	if _, statErr := os.Stat(outputPath); statErr == nil {
		t.Error("MP4 output file should not exist after unavailable error")
	}
}

func TestPureMuxMuxerUnavailableWhenNoFallbackForMetadata(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.webm")
	if err := writeMinimalWebM(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}

	m := NewPureMuxMuxer(nil)
	err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{Title: "x"})
	if !errors.Is(err, ErrPureMuxUnavailable) {
		t.Fatalf("Merge err = %v, want ErrPureMuxUnavailable", err)
	}
}

func TestPureMuxMuxerFallsBackWhenFFmpegUnavailable(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.mp4")
	if err := writeMinimalWebM(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalWebM(audioPath, false); err != nil {
		t.Fatal(err)
	}

	fb := &fakeMuxer{available: false}
	m := NewPureMuxMuxer(fb)
	err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{})
	if !errors.Is(err, ErrPureMuxUnavailable) {
		t.Fatalf("Merge err = %v, want ErrPureMuxUnavailable", err)
	}
	if fb.merged != 0 {
		t.Errorf("fallback should not be called when unavailable, got %d", fb.merged)
	}
}

func TestPureMuxMuxerValidatesPaths(t *testing.T) {
	m := NewPureMuxMuxer(nil)
	err := m.Merge(context.Background(), "http://example.com/v.webm", "audio.webm", "out.webm", types.Metadata{})
	if err == nil {
		t.Fatal("expected path validation error for http:// video path")
	}
}
