package client

// ExtractionEvent represents one extraction-stage lifecycle event.
type ExtractionEvent struct {
	Stage  string
	Phase  string
	Client string
	Detail string
}

// DownloadEvent represents one download lifecycle event.
type DownloadEvent struct {
	Stage   string
	Phase   string
	VideoID string
	Path    string
	Detail  string
}

// DownloadProgressEvent reports byte-level progress for a media download.
type DownloadProgressEvent struct {
	VideoID        string
	Path           string
	Itag           int
	Part           string
	Downloaded     int64
	Total          int64
	BytesPerSecond int64
}

// Logger is an optional package logger used for non-fatal warnings.
type Logger interface {
	// Warnf logs a formatted warning message.
	Warnf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Warnf(string, ...any) {}
