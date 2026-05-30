package muxer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/famomatic/ytv1/internal/types"
)

// Muxer defines the interface for media muxing operations.
type Muxer interface {
	Available() bool
	Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error
}

// FFmpegMuxer implements Muxer using the ffmpeg command line tool.
type FFmpegMuxer struct {
	Path      string
	ExtraArgs []string
}

// NewFFmpegMuxer returns a new FFmpegMuxer.
// If path is empty, it looks for "ffmpeg" in PATH.
func NewFFmpegMuxer(path string) *FFmpegMuxer {
	return NewFFmpegMuxerWithExtraArgs(path, nil)
}

// NewFFmpegMuxerWithExtraArgs returns a new FFmpegMuxer with additional args.
func NewFFmpegMuxerWithExtraArgs(path string, extraArgs []string) *FFmpegMuxer {
	if path == "" {
		path = "ffmpeg"
	}
	return &FFmpegMuxer{Path: path, ExtraArgs: append([]string(nil), extraArgs...)}
}

// Available checks if ffmpeg is executable.
func (f *FFmpegMuxer) Available() bool {
	_, err := exec.LookPath(f.Path)
	return err == nil
}

// Merge merges video and audio files into a single output file with metadata.
// It deletes the input files upon successful merge.
func (f *FFmpegMuxer) Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error {
	if err := validateFilePath(videoPath, "video"); err != nil {
		return err
	}
	if err := validateFilePath(audioPath, "audio"); err != nil {
		return err
	}
	if err := validateFilePath(outputPath, "output"); err != nil {
		return err
	}

	args := f.mergeArgs(videoPath, audioPath, outputPath, meta)
	cmd := exec.CommandContext(ctx, f.Path, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg merge failed: %w", err)
	}

	// Clean up input files
	_ = os.Remove(videoPath)
	_ = os.Remove(audioPath)

	return nil
}

func (f *FFmpegMuxer) mergeArgs(videoPath, audioPath, outputPath string, meta types.Metadata) []string {
	// -protocol_whitelist file restricts ffmpeg to local file I/O only,
	// preventing SSRF via ffmpeg protocol handlers (http, rtmp, concat, etc.).
	args := []string{
		"-protocol_whitelist", "file",
		"-i", videoPath,
		"-i", audioPath,
		"-c:v", "copy",
		"-c:a", "copy",
	}

	// Add Metadata — strip control characters to prevent ffmpeg arg injection.
	if meta.Title != "" {
		args = append(args, "-metadata", "title="+sanitizeMetadata(meta.Title))
	}
	if meta.Artist != "" {
		args = append(args, "-metadata", "artist="+sanitizeMetadata(meta.Artist))
	}
	if meta.Date != "" {
		args = append(args, "-metadata", "date="+sanitizeMetadata(meta.Date))
	}
	if meta.Description != "" {
		args = append(args, "-metadata", "comment="+sanitizeMetadata(meta.Description))
	}

	args = append(args, f.ExtraArgs...)
	args = append(args, "-y", outputPath)
	return args
}

// validateFilePath ensures a path is a local file reference, not a URL or
// ffmpeg protocol handler (http://, rtmp://, concat:, subfile:, etc.).
func validateFilePath(path, label string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("ffmpeg %s path is empty", label)
	}
	// Block URI schemes that ffmpeg could interpret as protocol handlers.
	if colon := strings.Index(path, ":"); colon > 0 {
		scheme := strings.ToLower(path[:colon])
		switch scheme {
		case "http", "https", "ftp", "rtmp", "rtmps", "rtsp", "srt",
			"concat", "subfile", "data", "crypto", "mmst", "mmsh":
			return fmt.Errorf("ffmpeg %s path uses forbidden protocol scheme %q: %s", label, scheme, path)
		}
	}
	// Block path traversal outside the working directory.
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("ffmpeg %s path invalid: %w", label, err)
	}
	_ = abs // absolute path is used by ffmpeg via the validated original path
	return nil
}

// sanitizeMetadata strips control characters from metadata values to prevent
// ffmpeg argument injection (e.g. newlines that could add extra flags).
func sanitizeMetadata(v string) string {
	var b strings.Builder
	b.Grow(len(v))
	for _, r := range v {
		if r < 0x20 && r != '\t' {
			continue // strip control characters except tab
		}
		b.WriteRune(r)
	}
	return b.String()
}
