package downloader

import (
	"context"
	"io"
)

// Downloader is the interface for downloading a stream.
type Downloader interface {
	// Download downloads the stream to the specified writer.
	// It returns the number of bytes written and any error encountered.
	Download(ctx context.Context, w io.Writer) (int64, error)
}

// ProgressReporter is an interface for reporting download progress.
type ProgressReporter interface {
	OnProgress(bytesWritten int64, totalBytes int64)
}
