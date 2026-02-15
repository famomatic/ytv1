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

// Logger is an optional package logger used for non-fatal warnings.
type Logger interface {
	// Warnf logs a formatted warning message.
	Warnf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Warnf(string, ...any) {}
