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

// DownloadProgressEvent reports progress for a media download.
//
// Byte fields (Downloaded/Total) drive percent/ETA for direct downloads whose
// total size is known. Segmented HLS/DASH downloads have no upfront byte total
// but do know playback durations, so DownloadedSeconds/TotalSeconds provide a
// duration-based percent and ETASeconds an estimated time remaining. A consumer
// prefers the duration fields when TotalSeconds > 0.
type DownloadProgressEvent struct {
	VideoID        string
	Path           string
	Itag           int
	Part           string
	Downloaded     int64
	Total          int64
	BytesPerSecond int64
	// DownloadedSeconds/TotalSeconds are the playback duration downloaded so far
	// and the media total (0 when unknown).
	DownloadedSeconds float64
	TotalSeconds      float64
	// ETASeconds is the estimated wall-clock seconds remaining derived from
	// duration progress; -1 when unknown.
	ETASeconds int64
	// Final marks the terminal progress event emitted by the reporter's finish().
	// Consumers use it as the true end-of-download signal, since a duration-based
	// estimate can reach 100% before the last segment actually arrives.
	Final bool
}

// Logger is an optional package logger used for non-fatal warnings.
type Logger interface {
	// Warnf logs a formatted warning message.
	Warnf(format string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Warnf(string, ...any) {}
