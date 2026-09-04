// Package muxer provides media container muxers. FFmpegMuxer shells out to
// the ffmpeg binary; PureMuxMuxer performs pure-Go packet demuxing/remuxing
// through puremux v0.2 and falls back to FFmpeg for unsupported cases.
package muxer

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/famomatic/puremux/pkg/media"
	"github.com/famomatic/ytv1/internal/types"
)

// pureMuxUnsupportedSentinels are the puremux errors that mean "try a fallback
// muxer" rather than a hard failure.
var pureMuxUnsupportedSentinels = []error{
	media.ErrUnsupportedFormat,
	media.ErrUnsupportedCodec,
	media.ErrIncompatible,
}

// PureMuxMuxer merges video and audio files using the pure-Go puremux
// pipeline. It is always Available() (no external binary required) for
// supported MP4/WebM/MKV/MPEG-TS output. Metadata-embedding requests,
// unsupported codec combinations, and puremux failures are routed to the
// optional Fallback Muxer (typically *FFmpegMuxer) when present.
type PureMuxMuxer struct {
	// Fallback handles cases puremux cannot mux (e.g. metadata embedding) or
	// when puremux fails. May be nil; when nil, Merge returns
	// ErrPureMuxUnavailable for unsupported inputs instead of falling back.
	Fallback Muxer
}

// NewPureMuxMuxer returns a PureMuxMuxer with default configuration and the
// given fallback muxer. Pass nil fallback to require puremux-only merging.
func NewPureMuxMuxer(fallback Muxer) *PureMuxMuxer {
	return &PureMuxMuxer{Fallback: fallback}
}

// Available reports whether the muxer can perform a merge without inspecting
// inputs. puremux is always usable for the containers it supports, so this
// returns true even when no ffmpeg binary is installed. Callers that need a
// single "can this environment merge anything" answer should also consult the
// Fallback muxer.
func (m *PureMuxMuxer) Available() bool {
	return true
}

// Merge merges videoPath and audioPath into outputPath. The v0.2 media API
// probes and demuxes each input, then compressed packets are interleaved into
// a native media muxer. Unsupported cases and any failed pure-Go attempt fall
// back to Fallback when configured.
func (m *PureMuxMuxer) Merge(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error {
	if err := validateFilePath(videoPath, "video"); err != nil {
		return err
	}
	if err := validateFilePath(audioPath, "audio"); err != nil {
		return err
	}
	if err := validateFilePath(outputPath, "output"); err != nil {
		return err
	}

	// puremux cannot embed container metadata; route to FFmpeg when the caller
	// asked for tagging so the result still carries title/artist/etc.
	if metadataRequested(meta) {
		return m.fallbackOrUnavailable(ctx, videoPath, audioPath, outputPath, meta)
	}

	err := remuxMediaFiles(ctx, videoPath, audioPath, outputPath)
	if err == nil {
		// Clean up intermediates on success, matching FFmpegMuxer.Merge.
		_ = os.Remove(videoPath)
		_ = os.Remove(audioPath)
		return nil
	}

	if isPureMuxUnsupported(err) {
		// puremux declared it cannot handle this (input/output/codec) pair.
		// Fall back to FFmpeg if available.
		return m.fallbackOrUnavailable(ctx, videoPath, audioPath, outputPath, meta)
	}

	// Non-sentinel failure (corrupt input, write error, ...). Try FFmpeg as a
	// last resort if available; otherwise surface the puremux error.
	if m.Fallback != nil && m.Fallback.Available() {
		if ferr := m.Fallback.Merge(ctx, videoPath, audioPath, outputPath, meta); ferr != nil {
			return fmt.Errorf("puremux merge failed: %w; ffmpeg fallback also failed: %v", err, ferr)
		}
		return nil
	}
	// No fallback: remove any partial/corrupt output puremux may have written
	// so a failed merge does not leave a truncated file masquerading as output.
	_ = os.Remove(outputPath)
	return fmt.Errorf("puremux merge failed: %w", err)
}

// fallbackOrUnavailable delegates to Fallback or returns ErrPureMuxUnavailable.
func (m *PureMuxMuxer) fallbackOrUnavailable(ctx context.Context, videoPath, audioPath, outputPath string, meta types.Metadata) error {
	if m.Fallback != nil && m.Fallback.Available() {
		return m.Fallback.Merge(ctx, videoPath, audioPath, outputPath, meta)
	}
	return ErrPureMuxUnavailable
}

// isPureMuxUnsupported reports whether err is one of puremux's "cannot handle
// this pair" sentinels, which are safe fallback triggers.
func isPureMuxUnsupported(err error) bool {
	for _, sentinel := range pureMuxUnsupportedSentinels {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// metadataRequested reports whether the caller asked for metadata embedding.
// puremux v0.2 remuxes opaque payloads only and cannot tag containers, so any
// non-empty metadata field forces an FFmpeg fallback when embedding is wanted.
func metadataRequested(meta types.Metadata) bool {
	return meta.Title != "" || meta.Artist != "" || meta.Description != "" || meta.Date != ""
}

// ErrPureMuxUnavailable is returned when neither puremux nor a configured
// fallback can merge the given inputs.
var ErrPureMuxUnavailable = errors.New("muxer unavailable")
