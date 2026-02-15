package client

import (
	"context"
	"io"
)

// MP3TranscodeMetadata describes source stream attributes for MP3 transcoding.
type MP3TranscodeMetadata struct {
	VideoID        string
	SourceItag     int
	SourceMimeType string
}

// MP3Transcoder converts an input audio stream into MP3 bytes.
type MP3Transcoder interface {
	// TranscodeToMP3 reads source audio bytes and writes MP3 output to dst.
	TranscodeToMP3(ctx context.Context, src io.Reader, dst io.Writer, meta MP3TranscodeMetadata) (int64, error)
}
