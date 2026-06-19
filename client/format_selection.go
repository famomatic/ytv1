package client

import (
	"fmt"
	"strings"

	"github.com/famomatic/ytv1/internal/selector"
	"github.com/famomatic/ytv1/internal/types"
)

// SelectFormatsForDownloadOptions previews the media formats that Download would
// choose for the supplied options before transport, muxer, or filesystem work.
func SelectFormatsForDownloadOptions(formats []FormatInfo, options DownloadOptions) ([]FormatInfo, error) {
	if len(formats) == 0 {
		return nil, ErrNoPlayableFormats
	}
	formats = playableSelectionFormats(formats)
	if len(formats) == 0 {
		return nil, ErrNoPlayableFormats
	}

	if options.Itag > 0 {
		for _, f := range formats {
			if f.Itag == options.Itag {
				return []FormatInfo{f}, nil
			}
		}
		return nil, fmt.Errorf("%w: itag=%d", ErrNoPlayableFormats, options.Itag)
	}

	selStr := defaultFormatSelector(options)
	parsed, err := selector.Parse(selStr)
	if err != nil {
		return nil, &NoPlayableFormatsDetailError{
			Mode:           normalizeSelectionMode(options.Mode),
			Selector:       selStr,
			SelectionError: "selector parse failed: " + err.Error(),
		}
	}

	selected, err := selector.Select(formats, parsed)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, &NoPlayableFormatsDetailError{
			Mode:           normalizeSelectionMode(options.Mode),
			Selector:       selStr,
			SelectionError: "no formats matched selector",
		}
	}

	if selectionHasCiphered(selected) {
		nonCiphered := make([]types.FormatInfo, 0, len(formats))
		for _, f := range formats {
			if !f.Ciphered {
				nonCiphered = append(nonCiphered, f)
			}
		}
		if len(nonCiphered) > 0 {
			if alt, err := selector.Select(nonCiphered, parsed); err == nil && len(alt) > 0 && len(alt) >= len(selected) {
				selected = alt
			}
		}
	}

	return selected, nil
}

func defaultFormatSelector(options DownloadOptions) string {
	if strings.TrimSpace(options.FormatSelector) != "" {
		return options.FormatSelector
	}
	switch normalizeSelectionMode(options.Mode) {
	case SelectionModeAudioOnly, SelectionModeMP3:
		return "bestaudio/best"
	case SelectionModeVideoOnly:
		return "bestvideo"
	case SelectionModeMP4VideoOnly:
		return "bestvideo[ext=mp4]"
	case SelectionModeMP4AV:
		return "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best"
	default:
		return "bestvideo+bestaudio/best"
	}
}

func playableSelectionFormats(formats []FormatInfo) []FormatInfo {
	out := make([]FormatInfo, 0, len(formats))
	for _, f := range formats {
		if f.IsDRM || f.IsDamaged {
			continue
		}
		out = append(out, f)
	}
	return out
}
