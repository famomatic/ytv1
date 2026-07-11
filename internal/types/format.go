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
	IsDRM        bool
	IsDamaged    bool
	Quality      string
	QualityLabel string
	SourceClient string
	// ContentLength is the expected byte size of the media stream as reported
	// by YouTube (0 when unknown). Used for post-download integrity checks.
	ContentLength int64
}
