package muxer

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/types"
)

// Uses distinguishable audio in the AV input and the selected audio input, so
// the assertion verifies real FFmpeg stream selection, not just argument text.
func TestFFmpegSelectedAudioIntegration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	video := filepath.Join(dir, "video.mp4")
	audio := filepath.Join(dir, "audio.m4a")
	out := filepath.Join(dir, "out.mp4")
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, ffmpeg, args...)
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("ffmpeg: %v: %s", err, data)
		}
		return data
	}
	run("-v", "error", "-f", "lavfi", "-i", "color=size=32x32:rate=10", "-f", "lavfi", "-i", "sine=frequency=220:sample_rate=8000", "-t", "1", "-c:v", "mpeg4", "-c:a", "aac", "-y", video)
	run("-v", "error", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=8000", "-t", "1", "-c:a", "aac", "-y", audio)
	if err := NewFFmpegMuxer(ffmpeg).Merge(ctx, video, audio, out, types.Metadata{}); err != nil {
		t.Fatal(err)
	}
	pcm := run("-v", "error", "-i", out, "-map", "0:a:0", "-ar", "8000", "-ac", "1", "-f", "s16le", "pipe:1")
	crossings := 0
	for i := 2; i+1 < len(pcm); i += 2 {
		a := int16(binary.LittleEndian.Uint16(pcm[i-2 : i]))
		b := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		if a <= 0 && b > 0 {
			crossings++
		}
	}
	if crossings < 600 {
		t.Fatalf("selected 880Hz audio lost (zero crossings=%d)", crossings)
	}
	previous, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// WAV cannot mux the selected video stream; failure occurs in a real
	// FFmpeg process, while the successful MP4 must remain intact.
	failing := NewFFmpegMuxerWithExtraArgs(ffmpeg, []string{"-f", "wav"})
	if err := failing.Merge(ctx, video, audio, out, types.Metadata{}); err == nil {
		t.Fatal("expected incompatible muxer failure")
	}
	current, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(previous, current) {
		t.Fatal("failed FFmpeg destroyed previous output", err)
	}
}
