package client

import (
	"mime"
	"strings"
)

// SelectionMode controls how a downloadable format is chosen when itag is not forced.
type SelectionMode string

const (
	SelectionModeBest         SelectionMode = "best"
	SelectionModeMP4AV        SelectionMode = "mp4av"
	SelectionModeMP4VideoOnly SelectionMode = "mp4videoonly"
	SelectionModeVideoOnly    SelectionMode = "videoonly" // Any container (webm/mp4)
	SelectionModeAudioOnly    SelectionMode = "audioonly" // Any container (webm/m4a/mp3 via transcoding)
	SelectionModeMP3          SelectionMode = "mp3"
)

func normalizeSelectionMode(mode SelectionMode) SelectionMode {
	switch SelectionMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case "", SelectionModeBest:
		return SelectionModeBest
	case SelectionModeMP4AV:
		return SelectionModeMP4AV
	case SelectionModeMP4VideoOnly:
		return SelectionModeMP4VideoOnly
	case SelectionModeVideoOnly:
		return SelectionModeVideoOnly
	case SelectionModeAudioOnly:
		return SelectionModeAudioOnly
	case SelectionModeMP3:
		return SelectionModeMP3
	default:
		return SelectionModeBest
	}
}

func selectDownloadFormat(formats []FormatInfo, opts DownloadOptions) (FormatInfo, bool) {
	if len(formats) == 0 {
		return FormatInfo{}, false
	}

	if opts.Itag != 0 {
		for _, f := range formats {
			if f.Itag == opts.Itag {
				return f, true
			}
		}
		return FormatInfo{}, false
	}

	mode := normalizeSelectionMode(opts.Mode)
	var best FormatInfo
	hasBest := false

	for _, f := range formats {
		if !matchesSelectionMode(f, mode) {
			continue
		}
		if !hasBest || betterForMode(f, best, mode) {
			best = f
			hasBest = true
		}
	}

	return best, hasBest
}

func matchesSelectionMode(f FormatInfo, mode SelectionMode) bool {
	container := mimeContainer(f.MimeType)
	switch mode {
	case SelectionModeBest:
		return f.HasAudio || f.HasVideo
	case SelectionModeMP4AV:
		return container == "mp4" && f.HasAudio && f.HasVideo
	case SelectionModeMP4VideoOnly:
		return container == "mp4" && f.HasVideo && !f.HasAudio
	case SelectionModeVideoOnly:
		return f.HasVideo && !f.HasAudio
	case SelectionModeAudioOnly, SelectionModeMP3:
		return f.HasAudio && !f.HasVideo
	default:
		return f.HasAudio || f.HasVideo
	}
}

func betterForMode(a, b FormatInfo, mode SelectionMode) bool {
	switch mode {
	case SelectionModeAudioOnly, SelectionModeMP3:
		return compareKeys(
			[]int{a.Bitrate, boolScore(a.Ciphered), -a.Itag},
			[]int{b.Bitrate, boolScore(b.Ciphered), -b.Itag},
		)
	case SelectionModeMP4AV, SelectionModeMP4VideoOnly:
		return compareKeys(
			[]int{a.Height, a.Width, a.FPS, a.Bitrate, boolScore(a.Ciphered), -a.Itag},
			[]int{b.Height, b.Width, b.FPS, b.Bitrate, boolScore(b.Ciphered), -b.Itag},
		)
	case SelectionModeVideoOnly:
		return compareKeys(
			[]int{a.Height, a.Width, a.FPS, a.Bitrate, boolScore(a.Ciphered), -a.Itag},
			[]int{b.Height, b.Width, b.FPS, b.Bitrate, boolScore(b.Ciphered), -b.Itag},
		)
	case SelectionModeBest:
		return compareKeys(
			[]int{trackRank(a), a.Height, a.Width, a.FPS, a.Bitrate, boolScore(a.Ciphered), -a.Itag},
			[]int{trackRank(b), b.Height, b.Width, b.FPS, b.Bitrate, boolScore(b.Ciphered), -b.Itag},
		)
	default:
		return compareKeys(
			[]int{trackRank(a), a.Height, a.Width, a.FPS, a.Bitrate, boolScore(a.Ciphered), -a.Itag},
			[]int{trackRank(b), b.Height, b.Width, b.FPS, b.Bitrate, boolScore(b.Ciphered), -b.Itag},
		)
	}
}

func compareKeys(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		return a[i] > b[i]
	}
	return false
}

func trackRank(f FormatInfo) int {
	switch {
	case f.HasVideo && f.HasAudio:
		return 3
	case f.HasVideo:
		return 2
	case f.HasAudio:
		return 1
	default:
		return 0
	}
}

func boolScore(ciphered bool) int {
	if ciphered {
		return 0
	}
	return 1
}

func mimeContainer(mimeType string) string {
	mediaType, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(mediaType, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}
