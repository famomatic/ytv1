package client

import (
	"fmt"
	"mime"
	"strings"
)

// FormatMediaExt returns the media subtype label used in human format lists.
func FormatMediaExt(mimeType string) string {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return "?"
	}
	parts := strings.SplitN(mediaType, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "?"
	}
	return parts[1]
}

// FormatTrackNote describes whether a format carries audio, video, or both.
func FormatTrackNote(format FormatInfo) string {
	switch {
	case format.HasAudio && !format.HasVideo:
		return "audio only"
	case format.HasVideo && !format.HasAudio:
		return "video only"
	case format.HasAudio && format.HasVideo:
		return "av"
	default:
		return ""
	}
}

// FormatSummary renders the compact selected-format string used by --get-format.
func FormatSummary(format FormatInfo) string {
	ext := FormatMediaExt(format.MimeType)
	resolution := "audio only"
	if format.HasVideo {
		resolution = fmt.Sprintf("%dx%d", format.Width, format.Height)
	}
	note := FormatTrackNote(format)
	if format.Incomplete {
		note = strings.TrimSpace(note + " incomplete")
	}
	if note == "" {
		return fmt.Sprintf("%d - %s %s", format.Itag, ext, resolution)
	}
	return fmt.Sprintf("%d - %s %s %s", format.Itag, ext, resolution, note)
}

// FormatSummaries joins multiple compact selected-format strings in merge order.
func FormatSummaries(formats []FormatInfo) string {
	parts := make([]string, 0, len(formats))
	for _, format := range formats {
		parts = append(parts, FormatSummary(format))
	}
	return strings.Join(parts, " + ")
}
