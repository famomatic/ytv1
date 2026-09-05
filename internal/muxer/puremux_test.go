package muxer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/famomatic/puremux/pkg/media"
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

	if _, err := os.Stat(videoPath); err != nil {
		t.Errorf("video input should remain owned by caller, stat err=%v", err)
	}
	if _, err := os.Stat(audioPath); err != nil {
		t.Errorf("audio input should remain owned by caller, stat err=%v", err)
	}
}

func TestPureMuxMuxerFallsBackOnUnsupportedOutput(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.webm")
	audioPath := filepath.Join(dir, "audio.webm")
	outputPath := filepath.Join(dir, "merged.avi")
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

func TestPureMuxMuxerMergesMP4WithoutFallback(t *testing.T) {
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
	if err := m.Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	source, err := media.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	demuxer, err := media.Open(context.Background(), source, media.OpenOptions{})
	if err != nil {
		_ = source.Close()
		t.Fatalf("open native MP4: %v", err)
	}
	defer demuxer.Close()
	if demuxer.Info().Format != media.FormatMP4 || len(demuxer.Streams()) != 2 {
		t.Fatalf("native MP4 info=%+v streams=%+v", demuxer.Info(), demuxer.Streams())
	}
}

func TestPureMuxMuxerMergesH264AACMP4WithoutFallback(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "video.mp4")
	audioPath := filepath.Join(dir, "audio.m4a")
	outputPath := filepath.Join(dir, "merged.mp4")
	if err := writeMinimalMP4Track(videoPath, true); err != nil {
		t.Fatal(err)
	}
	if err := writeMinimalMP4Track(audioPath, false); err != nil {
		t.Fatal(err)
	}
	fallback := &fakeMuxer{available: true}
	if err := NewPureMuxMuxer(fallback).Merge(context.Background(), videoPath, audioPath, outputPath, types.Metadata{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if fallback.merged != 0 {
		t.Fatalf("FFmpeg fallback merged %d times, want native MP4 path", fallback.merged)
	}
	source, err := media.OpenFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	demuxer, err := media.Open(context.Background(), source, media.OpenOptions{})
	if err != nil {
		_ = source.Close()
		t.Fatal(err)
	}
	defer demuxer.Close()
	var h264, aac bool
	for _, stream := range demuxer.Streams() {
		h264 = h264 || stream.Type == media.MediaVideo && stream.Codec == media.CodecH264
		aac = aac || stream.Type == media.MediaAudio && stream.Codec == media.CodecAAC
	}
	if !h264 || !aac {
		t.Fatalf("native MP4 streams = %+v", demuxer.Streams())
	}
}

func writeMinimalMP4Track(path string, video bool) (retErr error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); retErr == nil {
			retErr = err
		}
	}()
	mux, err := media.NewMuxer(f, media.MuxOptions{Format: media.FormatMP4})
	if err != nil {
		return err
	}
	stream := media.Stream{
		Type: media.MediaAudio, Codec: media.CodecAAC,
		TimeBase: media.Rational{Num: 1, Den: 44_100}, SampleRate: 44_100, Channels: 2,
		Config: media.CodecConfig{Format: media.CodecConfigASC, Data: []byte{0x12, 0x10}},
	}
	packet := &media.Packet{
		Data: []byte{0x21, 0x10, 0x56}, PTS: media.KnownTimestamp(0), DTS: media.KnownTimestamp(0),
		Duration: media.KnownTimestamp(1_024), Flags: media.PacketKeyframe,
	}
	if video {
		stream = media.Stream{
			Type: media.MediaVideo, Codec: media.CodecH264,
			TimeBase: media.Rational{Num: 1, Den: 90_000}, Width: 320, Height: 180,
			Config: media.CodecConfig{Format: media.CodecConfigAVCC,
				Data: []byte{1, 0x42, 0, 0x1f, 0xff, 0xe1, 0, 1, 0x67, 1, 0, 1, 0x68}},
		}
		packet.Data = []byte{0, 0, 0, 1, 0x65}
		packet.Duration = media.KnownTimestamp(3_000)
	}
	packet.StreamIndex, err = mux.AddStream(stream)
	if err != nil {
		_ = mux.Close()
		return err
	}
	if err := mux.WritePacket(context.Background(), packet); err != nil {
		_ = mux.Close()
		return err
	}
	return mux.Close()
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
	outputPath := filepath.Join(dir, "merged.avi")
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
