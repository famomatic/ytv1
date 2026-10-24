package client

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// SubtitleOutputFormat is a transcript serialization target format.
type SubtitleOutputFormat string

const (
	SubtitleOutputFormatSRT SubtitleOutputFormat = "srt"
	SubtitleOutputFormatVTT SubtitleOutputFormat = "vtt"
)

// ResolveSubtitleOutputFormat selects an output format from yt-dlp style preferences
// such as "vtt/srt" or "best". Unknown values fall back to SRT.
func ResolveSubtitleOutputFormat(raw string) SubtitleOutputFormat {
	tokens := strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool {
		return r == '/' || r == ','
	})
	for _, token := range tokens {
		switch strings.TrimSpace(token) {
		case "best", "srt":
			return SubtitleOutputFormatSRT
		case "vtt":
			return SubtitleOutputFormatVTT
		}
	}
	return SubtitleOutputFormatSRT
}

// WriteTranscript serializes transcript entries to the selected subtitle format.
func WriteTranscript(path string, transcript *Transcript, format SubtitleOutputFormat) error {
	if transcript == nil {
		return fmt.Errorf("transcript is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ytv1-subtitle-*")
	if err != nil {
		return err
	}
	target := tmp.Name()
	defer os.Remove(target)
	if err := tmp.Close(); err != nil {
		return err
	}
	if format == SubtitleOutputFormatVTT {
		err = writeTranscriptAsVTT(target, transcript)
	} else {
		err = writeTranscriptAsSRT(target, transcript)
	}
	if err != nil {
		return err
	}
	return os.Rename(target, path)
}

func writeTranscriptAsSRT(path string, transcript *Transcript) error {
	if transcript == nil {
		return fmt.Errorf("transcript is nil")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	for i, entry := range transcript.Entries {
		start := formatSRTTimestamp(entry.StartSec)
		end := formatSRTTimestamp(entry.StartSec + entry.DurSec)
		if _, err := fmt.Fprintf(f, "%d\n%s --> %s\n%s\n\n", i+1, start, end, strings.TrimSpace(entry.Text)); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func writeTranscriptAsVTT(path string, transcript *Transcript) error {
	if transcript == nil {
		return fmt.Errorf("transcript is nil")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, "WEBVTT"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f); err != nil {
		return err
	}
	for _, entry := range transcript.Entries {
		start := formatVTTTimestamp(entry.StartSec)
		end := formatVTTTimestamp(entry.StartSec + entry.DurSec)
		if _, err := fmt.Fprintf(f, "%s --> %s\n%s\n\n", start, end, strings.TrimSpace(entry.Text)); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func formatSRTTimestamp(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	totalMs := int64(math.Round(sec * 1000))
	hours := totalMs / (60 * 60 * 1000)
	minutes := (totalMs / (60 * 1000)) % 60
	seconds := (totalMs / 1000) % 60
	millis := totalMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, seconds, millis)
}

func formatVTTTimestamp(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	totalMs := int64(math.Round(sec * 1000))
	hours := totalMs / (60 * 60 * 1000)
	minutes := (totalMs / (60 * 1000)) % 60
	seconds := (totalMs / 1000) % 60
	millis := totalMs % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, millis)
}
