package muxer

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/famomatic/ytv1/internal/types"
)

// Muxer defines the interface for media muxing operations.
type Muxer interface {
	Available() bool
	Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error
}

// FFmpegMuxer implements Muxer using the ffmpeg command line tool.
type FFmpegMuxer struct {
	Path string
}

// NewFFmpegMuxer returns a new FFmpegMuxer.
// If path is empty, it looks for "ffmpeg" in PATH.
func NewFFmpegMuxer(path string) *FFmpegMuxer {
	if path == "" {
		path = "ffmpeg"
	}
	return &FFmpegMuxer{Path: path}
}

// Available checks if ffmpeg is executable.
func (f *FFmpegMuxer) Available() bool {
	_, err := exec.LookPath(f.Path)
	return err == nil
}

// Merge merges video and audio files into a single output file with metadata.
// It deletes the input files upon successful merge.
func (f *FFmpegMuxer) Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error {
	// ffmpeg -i video.mp4 -i audio.m4a -c:v copy -c:a copy -metadata title=... -y output.mp4
	args := []string{
		"-i", videoPath,
		"-i", audioPath,
		"-c:v", "copy",
		"-c:a", "copy",
	}

	// Add Metadata
	if meta.Title != "" {
		args = append(args, "-metadata", "title="+meta.Title)
	}
	if meta.Artist != "" {
		args = append(args, "-metadata", "artist="+meta.Artist)
	}
	if meta.Date != "" {
		args = append(args, "-metadata", "date="+meta.Date)
		// Also standard creation_time?
		// args = append(args, "-metadata", "creation_time="+meta.Date)
	}
	if meta.Description != "" {
		args = append(args, "-metadata", "comment="+meta.Description)
	}

	args = append(args, "-y", outputPath)

	cmd := exec.CommandContext(ctx, f.Path, args...)
	cmd.Stdout = nil // or pipe to logger
	cmd.Stderr = nil // or pipe to logger

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg merge failed: %w", err)
	}

	// Clean up input files
	_ = os.Remove(videoPath)
	_ = os.Remove(audioPath)

	return nil
}
