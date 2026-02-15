package formats

import (
	"mime"
	"net/url"
	"strconv"
	"strings"

	"github.com/mjmst/ytv1/internal/innertube"
)

// Format represents a media format.
type Format struct {
	Itag             int
	URL              string
	MimeType         string
	Container        string
	Codecs           []string
	Bitrate          int
	Width            int
	Height           int
	FPS              int
	Quality          string
	QualityLabel     string
	AudioQuality     string
	AudioSampleRate  int
	AudioChannels    int
	ApproxDurationMs int64
	LastModified     string
	ContentLength    int64
	InitRange        *Range
	IndexRange       *Range
	ProjectionType   string
	AverageBitrate   int
	ThisIsLive       bool
	Protocol         string // "https", "dash", "hls"
	HasAudio         bool
	HasVideo         bool
	Ciphered         bool
	SignatureCipher  string
	Cipher           string
}

type Range struct {
	Start int64
	End   int64
}

// Parse extracts normalized formats from a PlayerResponse.
func Parse(resp *innertube.PlayerResponse) []Format {
	if resp == nil {
		return nil
	}

	formats := make([]Format, 0, len(resp.StreamingData.Formats)+len(resp.StreamingData.AdaptiveFormats))
	isLive := resp.PlayabilityStatus.IsLive()

	extract := func(raw []innertube.Format, adaptive bool) {
		for _, f := range raw {
			container, codecs := parseMimeDetails(f.MimeType)
			parsed := Format{
				Itag:             f.Itag,
				URL:              f.URL,
				MimeType:         f.MimeType,
				Container:        container,
				Codecs:           codecs,
				Bitrate:          f.Bitrate,
				Width:            f.Width,
				Height:           f.Height,
				FPS:              f.FPS,
				Quality:          f.Quality,
				QualityLabel:     f.QualityLabel,
				AudioQuality:     f.AudioQuality,
				AudioChannels:    f.AudioChannels,
				LastModified:     f.LastModified,
				ProjectionType:   f.ProjectionType,
				AverageBitrate:   f.AverageBitrate,
				ThisIsLive:       isLive,
				Protocol:         normalizeProtocol(f.URL),
				SignatureCipher:  f.SignatureCipher,
				Cipher:           f.Cipher,
				AudioSampleRate:  parseInt(f.AudioSampleRate),
				ApproxDurationMs: parseInt64(f.ApproxDurationMs),
				ContentLength:    parseInt64(f.ContentLength),
				InitRange:        parseRange(f.InitRange),
				IndexRange:       parseRange(f.IndexRange),
			}

			parsed.Ciphered = parsed.URL == "" && (parsed.SignatureCipher != "" || parsed.Cipher != "")
			parsed.HasAudio, parsed.HasVideo = deriveMediaFlags(parsed, adaptive)

			formats = append(formats, parsed)
		}
	}

	extract(resp.StreamingData.Formats, false)
	extract(resp.StreamingData.AdaptiveFormats, true)

	return formats
}

func parseInt(raw string) int {
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

func parseInt64(raw string) int64 {
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseRange(r *innertube.Range) *Range {
	if r == nil {
		return nil
	}
	return &Range{
		Start: parseInt64(r.Start),
		End:   parseInt64(r.End),
	}
}

func parseMimeDetails(raw string) (container string, codecs []string) {
	mediaType, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", nil
	}

	if parts := strings.SplitN(mediaType, "/", 2); len(parts) == 2 {
		container = strings.ToLower(parts[1])
	}

	if rawCodecs, ok := params["codecs"]; ok {
		for _, codec := range strings.Split(rawCodecs, ",") {
			codec = strings.TrimSpace(codec)
			if codec != "" {
				codecs = append(codecs, codec)
			}
		}
	}

	return container, codecs
}

func deriveMediaFlags(f Format, adaptive bool) (hasAudio bool, hasVideo bool) {
	mimeType := strings.ToLower(f.MimeType)

	if strings.HasPrefix(mimeType, "audio/") {
		hasAudio = true
	}
	if strings.HasPrefix(mimeType, "video/") {
		hasVideo = true
	}

	if f.AudioChannels > 0 || f.AudioSampleRate > 0 {
		hasAudio = true
	}
	if f.Width > 0 || f.Height > 0 || f.FPS > 0 {
		hasVideo = true
	}

	for _, codec := range f.Codecs {
		lc := strings.ToLower(codec)
		if strings.HasPrefix(lc, "mp4a") || strings.HasPrefix(lc, "opus") || strings.HasPrefix(lc, "vorbis") || strings.HasPrefix(lc, "aac") {
			hasAudio = true
		}
		if strings.HasPrefix(lc, "avc1") || strings.HasPrefix(lc, "av01") || strings.HasPrefix(lc, "vp9") || strings.HasPrefix(lc, "vp8") || strings.HasPrefix(lc, "hev1") || strings.HasPrefix(lc, "hvc1") {
			hasVideo = true
		}
	}

	// Progressive entries (non-adaptive) usually include both tracks.
	if !adaptive && hasVideo && !hasAudio {
		hasAudio = true
	}

	return hasAudio, hasVideo
}

func normalizeProtocol(rawURL string) string {
	if rawURL == "" {
		return "https"
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return "https"
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return "https"
	case "dash":
		return "dash"
	case "hls":
		return "hls"
	default:
		return "https"
	}
}
