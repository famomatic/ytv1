package client

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{name: "invalid input", err: ErrInvalidInput, want: ErrorCategoryInvalidInput},
		{name: "login required", err: ErrLoginRequired, want: ErrorCategoryLoginRequired},
		{name: "unavailable", err: ErrUnavailable, want: ErrorCategoryUnavailable},
		{name: "no playable", err: ErrNoPlayableFormats, want: ErrorCategoryNoPlayableFormats},
		{name: "challenge", err: ErrChallengeNotSolved, want: ErrorCategoryChallengeNotSolved},
		{name: "all clients", err: ErrAllClientsFailed, want: ErrorCategoryAllClientsFailed},
		{name: "mp3", err: ErrMP3TranscoderNotConfigured, want: ErrorCategoryMP3TranscoderNotConfigured},
		{name: "transcript parse", err: ErrTranscriptParse, want: ErrorCategoryTranscriptParse},
		{name: "download detail", err: &DownloadFailureDetailError{}, want: ErrorCategoryDownloadFailed},
		{name: "unknown", err: errors.New("boom"), want: ErrorCategoryUnknown},
	}
	for _, tt := range tests {
		got := ClassifyError(tt.err)
		if got != tt.want {
			t.Fatalf("%s: ClassifyError()=%q want=%q", tt.name, got, tt.want)
		}
	}
}
