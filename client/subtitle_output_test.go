package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSubtitleOutputFormat(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want SubtitleOutputFormat
	}{
		{name: "vtt preferred", raw: "vtt/srt", want: SubtitleOutputFormatVTT},
		{name: "best fallback", raw: "best", want: SubtitleOutputFormatSRT},
		{name: "unknown fallback", raw: "srv3/ttml", want: SubtitleOutputFormatSRT},
		{name: "empty fallback", raw: "", want: SubtitleOutputFormatSRT},
	}
	for _, tt := range tests {
		got := ResolveSubtitleOutputFormat(tt.raw)
		if got != tt.want {
			t.Fatalf("%s: ResolveSubtitleOutputFormat(%q)=%q want=%q", tt.name, tt.raw, got, tt.want)
		}
	}
}

func TestWriteTranscript_SRT(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub.srt")
	err := WriteTranscript(out, &Transcript{
		Entries: []TranscriptEntry{
			{StartSec: 0.0, DurSec: 1.5, Text: "hello"},
			{StartSec: 1.5, DurSec: 0.5, Text: "world"},
		},
	}, SubtitleOutputFormatSRT)
	if err != nil {
		t.Fatalf("WriteTranscript(SRT) error = %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	txt := string(raw)
	if !strings.Contains(txt, "00:00:00,000 --> 00:00:01,500") {
		t.Fatalf("unexpected srt output: %q", txt)
	}
}

func TestWriteTranscript_VTT(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sub.vtt")
	err := WriteTranscript(out, &Transcript{
		Entries: []TranscriptEntry{
			{StartSec: 0.0, DurSec: 1.5, Text: "hello"},
			{StartSec: 1.5, DurSec: 0.5, Text: "world"},
		},
	}, SubtitleOutputFormatVTT)
	if err != nil {
		t.Fatalf("WriteTranscript(VTT) error = %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	txt := string(raw)
	if !strings.Contains(txt, "WEBVTT") {
		t.Fatalf("unexpected vtt output: %q", txt)
	}
	if !strings.Contains(txt, "00:00:00.000 --> 00:00:01.500") {
		t.Fatalf("unexpected vtt output: %q", txt)
	}
}
