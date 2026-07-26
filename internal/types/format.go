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
	// Parts holds ordered HLS media-playlist URLs when a single logical stream
	// is split across multiple parts (e.g. a SOOP VOD returned as several
	// files). Each part is downloaded sequentially and concatenated into one
	// output. When empty or of length 1, URL is the single playlist. Parts[0]
	// always equals URL when set.
	Parts []string
}
