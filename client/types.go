package client

// VideoInfo is the package-level metadata result.
type VideoInfo struct {
	ID      string
	Title   string
	Author  string
	Formats []FormatInfo
}

// FormatInfo is the normalized public format model.
type FormatInfo struct {
	Itag         int
	URL          string
	MimeType     string
	HasAudio     bool
	HasVideo     bool
	Bitrate      int
	Width        int
	Height       int
	FPS          int
	Ciphered     bool
	Quality      string
	QualityLabel string
}
