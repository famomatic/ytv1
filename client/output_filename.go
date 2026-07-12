package client

import (
	"fmt"
	"mime"
	"path/filepath"
	"strconv"
	"strings"
)

// OutputFilenameOptions controls package-level output filename prediction.
type OutputFilenameOptions struct {
	OutputTemplate    string
	Mode              SelectionMode
	MergeOutputExt    string
	TrimFilenames     int
	RestrictFilenames bool
}

// PredictOutputFilename predicts the concrete output path for selected formats.
func PredictOutputFilename(info *VideoInfo, selected []FormatInfo, opts OutputFilenameOptions) (string, error) {
	if info == nil {
		return "", fmt.Errorf("%w: missing video info", ErrInvalidInput)
	}
	if len(selected) == 0 {
		return "", fmt.Errorf("%w: no selected formats", ErrNoPlayableFormats)
	}
	mergeExt := NormalizeMergeOutputExt(opts.MergeOutputExt)
	if IsMergedSelection(selected) {
		// Mirror download.go: when no explicit container was requested, prefer
		// webm for WebM pairs so puremux (pure Go) can merge without ffmpeg.
		if strings.TrimSpace(opts.MergeOutputExt) == "" && selectionIsWebM(selected) {
			mergeExt = "webm"
		}
		videoItag, audioItag := MergedItags(selected)
		defaultPath := defaultOutputFilenameFromTemplate(info, mergeExt, fmt.Sprintf("%d+%d", videoItag, audioItag), selected, opts)
		if strings.TrimSpace(opts.OutputTemplate) == "" {
			return TrimOutputPathFilename(defaultPath, opts.TrimFilenames), nil
		}
		formatTokens := SelectedFormatTemplateTokens(selected)
		rendered := RenderOutputTemplate(opts.OutputTemplate, OutputTemplateData{
			VideoID:           info.ID,
			Title:             info.Title,
			Uploader:          info.Author,
			UploaderID:        info.ChannelID,
			Channel:           info.Author,
			ChannelID:         info.ChannelID,
			Ext:               mergeExt,
			Itag:              fmt.Sprintf("%d+%d", videoItag, audioItag),
			FormatID:          fmt.Sprintf("%d+%d", videoItag, audioItag),
			Resolution:        formatTokens.Resolution,
			Width:             formatTokens.Width,
			Height:            formatTokens.Height,
			FPS:               formatTokens.FPS,
			TBR:               formatTokens.TBR,
			VBR:               formatTokens.VBR,
			ABR:               formatTokens.ABR,
			Protocol:          formatTokens.Protocol,
			VCodec:            formatTokens.VCodec,
			ACodec:            formatTokens.ACodec,
			UploadDate:        templateUploadDate(info),
			ReleaseDate:       templateReleaseDate(info),
			Timestamp:         templateTimestamp(info),
			RestrictFilenames: opts.RestrictFilenames,
		})
		if strings.TrimSpace(rendered) == "" {
			return TrimOutputPathFilename(defaultPath, opts.TrimFilenames), nil
		}
		if filepath.Ext(rendered) == "" {
			rendered += "." + mergeExt
		}
		return TrimOutputPathFilename(rendered, opts.TrimFilenames), nil
	}

	format := selected[0]
	defaultPath := defaultOutputFilenameFromTemplate(info, DetectOutputExt(format.MimeType, opts.Mode), strconv.Itoa(format.Itag), selected, opts)
	if strings.TrimSpace(opts.OutputTemplate) == "" {
		return TrimOutputPathFilename(defaultPath, opts.TrimFilenames), nil
	}
	formatTokens := SelectedFormatTemplateTokens(selected)
	rendered := RenderOutputTemplate(opts.OutputTemplate, OutputTemplateData{
		VideoID:           info.ID,
		Title:             info.Title,
		Uploader:          info.Author,
		UploaderID:        info.ChannelID,
		Channel:           info.Author,
		ChannelID:         info.ChannelID,
		Ext:               DetectOutputExt(format.MimeType, opts.Mode),
		Itag:              strconv.Itoa(format.Itag),
		FormatID:          strconv.Itoa(format.Itag),
		Resolution:        formatTokens.Resolution,
		Width:             formatTokens.Width,
		Height:            formatTokens.Height,
		FPS:               formatTokens.FPS,
		TBR:               formatTokens.TBR,
		VBR:               formatTokens.VBR,
		ABR:               formatTokens.ABR,
		Protocol:          formatTokens.Protocol,
		VCodec:            formatTokens.VCodec,
		ACodec:            formatTokens.ACodec,
		UploadDate:        templateUploadDate(info),
		ReleaseDate:       templateReleaseDate(info),
		Timestamp:         templateTimestamp(info),
		RestrictFilenames: opts.RestrictFilenames,
	})
	if strings.TrimSpace(rendered) == "" {
		return TrimOutputPathFilename(defaultPath, opts.TrimFilenames), nil
	}
	return TrimOutputPathFilename(rendered, opts.TrimFilenames), nil
}

func defaultOutputFilenameFromTemplate(info *VideoInfo, ext string, itag string, selected []FormatInfo, opts OutputFilenameOptions) string {
	formatTokens := SelectedFormatTemplateTokens(selected)
	return RenderOutputTemplate(DefaultOutputTemplate, OutputTemplateData{
		VideoID:           info.ID,
		Title:             info.Title,
		Uploader:          info.Author,
		UploaderID:        info.ChannelID,
		Channel:           info.Author,
		ChannelID:         info.ChannelID,
		Ext:               ext,
		Itag:              itag,
		FormatID:          itag,
		Resolution:        formatTokens.Resolution,
		Width:             formatTokens.Width,
		Height:            formatTokens.Height,
		FPS:               formatTokens.FPS,
		TBR:               formatTokens.TBR,
		VBR:               formatTokens.VBR,
		ABR:               formatTokens.ABR,
		Protocol:          formatTokens.Protocol,
		VCodec:            formatTokens.VCodec,
		ACodec:            formatTokens.ACodec,
		UploadDate:        templateUploadDate(info),
		ReleaseDate:       templateReleaseDate(info),
		Timestamp:         templateTimestamp(info),
		RestrictFilenames: opts.RestrictFilenames,
	})
}

// IsMergedSelection reports whether selected formats represent video+audio merge output.
func IsMergedSelection(formats []FormatInfo) bool {
	if len(formats) < 2 {
		return false
	}
	hasVideo := false
	hasAudio := false
	for _, format := range formats {
		hasVideo = hasVideo || (format.HasVideo && !format.HasAudio)
		hasAudio = hasAudio || (format.HasAudio && !format.HasVideo)
	}
	return hasVideo && hasAudio
}

// MergedItags returns the first selected video and audio itags.
func MergedItags(formats []FormatInfo) (int, int) {
	videoItag := 0
	audioItag := 0
	for _, format := range formats {
		if videoItag == 0 && format.HasVideo {
			videoItag = format.Itag
		}
		if audioItag == 0 && format.HasAudio {
			audioItag = format.Itag
		}
	}
	return videoItag, audioItag
}

// DefaultOutputPath returns the package default output filename.
func DefaultOutputPath(videoID string, itag int, mimeType string, mode SelectionMode) string {
	if mode == SelectionModeMP3 {
		return fmt.Sprintf("%s-%d.mp3", videoID, itag)
	}
	ext := ".bin"
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		if parts := strings.SplitN(mediaType, "/", 2); len(parts) == 2 && parts[1] != "" {
			ext = "." + parts[1]
		}
	}
	return fmt.Sprintf("%s-%d%s", videoID, itag, ext)
}

// DetectOutputExt returns the output extension for a selected format/mode.
func DetectOutputExt(mimeType string, mode SelectionMode) string {
	if mode == SelectionModeMP3 {
		return "mp3"
	}
	if mediaType, _, err := mime.ParseMediaType(mimeType); err == nil {
		if parts := strings.SplitN(mediaType, "/", 2); len(parts) == 2 && parts[1] != "" {
			return parts[1]
		}
	}
	return "bin"
}

// NormalizeMergeOutputExt validates and normalizes a merge output extension.
func NormalizeMergeOutputExt(raw string) string {
	ext := strings.ToLower(strings.TrimSpace(raw))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		return "mp4"
	}
	for _, r := range ext {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return "mp4"
	}
	return ext
}

// RemuxVideoTargetExt derives the first practical target container from a
// yt-dlp-style remux rule string such as "webm>mp4/mkv".
func RemuxVideoTargetExt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "/")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.LastIndex(part, ">"); i >= 0 {
			part = strings.TrimSpace(part[i+1:])
		}
		if part != "" {
			return part
		}
	}
	return raw
}

// selectionIsWebM reports whether every selected format is a WebM stream
// (video/webm or audio/webm). Used to pick a webm merge container by default
// so the pure-Go puremux muxer can handle the merge without an ffmpeg binary.
func selectionIsWebM(selected []FormatInfo) bool {
	if len(selected) == 0 {
		return false
	}
	for _, f := range selected {
		mediaType, _, err := mime.ParseMediaType(f.MimeType)
		if err != nil {
			if !strings.Contains(strings.ToLower(f.MimeType), "webm") {
				return false
			}
			continue
		}
		if mediaType != "video/webm" && mediaType != "audio/webm" {
			return false
		}
	}
	return true
}

// EffectiveMergeOutputExt resolves explicit merge-output preference first,
// then a remux-video rule, and finally the package default.
func EffectiveMergeOutputExt(mergeOutputFormat string, remuxVideo string) string {
	if strings.TrimSpace(mergeOutputFormat) != "" {
		return NormalizeMergeOutputExt(mergeOutputFormat)
	}
	return NormalizeMergeOutputExt(RemuxVideoTargetExt(remuxVideo))
}

// TrimOutputPathFilename limits the final path basename while preserving directories/extensions.
func TrimOutputPathFilename(path string, limit int) string {
	if limit <= 0 {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if len([]rune(stem)) <= limit {
		return path
	}
	runes := []rune(stem)
	trimmed := string(runes[:limit]) + ext
	if dir == "." || dir == "" {
		return trimmed
	}
	return filepath.Join(dir, trimmed)
}

func templateUploadDate(info *VideoInfo) string {
	if info == nil {
		return ""
	}
	return compactDateToken(firstNonEmpty(info.UploadDate, info.PublishDate))
}

func templateReleaseDate(info *VideoInfo) string {
	if info == nil {
		return ""
	}
	return compactDateToken(firstNonEmpty(info.PublishDate, info.UploadDate))
}

func templateTimestamp(info *VideoInfo) string {
	if info == nil {
		return ""
	}
	date := compactDateToken(firstNonEmpty(info.UploadDate, info.PublishDate))
	if len(date) != 8 {
		return ""
	}
	return date + "000000"
}

func compactDateToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
