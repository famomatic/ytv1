package formats

import (
	"strconv"
	
	"github.com/mjmst/ytv1/internal/innertube"
)

// Format represents a media format.
type Format struct {
	Itag             int
	URL              string
	MimeType         string
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
}

type Range struct {
	Start int64
	End   int64
}

// Parse extracts formats from a PlayerResponse.
func Parse(resp *innertube.PlayerResponse) []Format {
	var formats []Format

	extract := func(raw []innertube.Format) {
		for _, f := range raw {
			parsed := Format{
				Itag:            f.Itag,
				URL:             f.URL,
				MimeType:        f.MimeType,
				Bitrate:         f.Bitrate,
				Width:           f.Width,
				Height:          f.Height,
				Quality:         f.Quality,
				QualityLabel:    f.QualityLabel,
				AudioQuality:    f.AudioQuality,
				AudioChannels:   f.AudioChannels,
				LastModified:    f.LastModified,
				ProjectionType:  f.ProjectionType,
				AverageBitrate:  f.AverageBitrate,
				Protocol:        "https", // Default
			}

			// Parse integers
			if len(f.QualityLabel) > 1 {
				if fps, _ := strconv.Atoi(f.QualityLabel[:len(f.QualityLabel)-1]); fps > 0 {
                    // Rough fps extraction, needs better regex usually but this is a placeholder
                    // Actually QualityLabel is like "1080p60", so we can extracting "60"
				}
			}
			
			if f.AudioSampleRate != "" {
				parsed.AudioSampleRate, _ = strconv.Atoi(f.AudioSampleRate)
			}
			if f.ApproxDurationMs != "" {
				parsed.ApproxDurationMs, _ = strconv.ParseInt(f.ApproxDurationMs, 10, 64)
			}
			if f.ContentLength != "" {
				parsed.ContentLength, _ = strconv.ParseInt(f.ContentLength, 10, 64)
			}

			if f.InitRange != nil {
				s, _ := strconv.ParseInt(f.InitRange.Start, 10, 64)
				e, _ := strconv.ParseInt(f.InitRange.End, 10, 64)
				parsed.InitRange = &Range{Start: s, End: e}
			}
			if f.IndexRange != nil {
				s, _ := strconv.ParseInt(f.IndexRange.Start, 10, 64)
				e, _ := strconv.ParseInt(f.IndexRange.End, 10, 64)
				parsed.IndexRange = &Range{Start: s, End: e}
			}

			// If URL is empty, it needs deciphering. For now, we assume URL is present or empty.
			// Ideally we should have a "Cipher" status.
			
			formats = append(formats, parsed)
		}
	}

	extract(resp.StreamingData.Formats)
	extract(resp.StreamingData.AdaptiveFormats)

	return formats
}
