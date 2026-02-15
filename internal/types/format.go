package types

// FormatInfo is the normalized public format model.
type FormatInfo struct {
	Itag         int
	URL          string
	MimeType     string
	Protocol     string
	HasAudio     bool
	HasVideo     bool
	Bitrate      int
	Width        int
	Height       int
	FPS          int
	Ciphered     bool
	Quality      string
	QualityLabel string
	SourceClient string
}
