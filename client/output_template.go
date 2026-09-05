package client

import (
	"fmt"
	"mime"
	"strconv"
	"strings"
)

// OutputTemplateData contains yt-dlp-style output template token values.
type OutputTemplateData struct {
	VideoID     string
	Title       string
	Uploader    string
	UploaderID  string
	Channel     string
	ChannelID   string
	Ext         string
	Itag        string
	FormatID    string
	Resolution  string
	Width       string
	Height      string
	FPS         string
	TBR         string
	VBR         string
	ABR         string
	Protocol    string
	VCodec      string
	ACodec      string
	UploadDate  string
	ReleaseDate string
	Timestamp   string

	RestrictFilenames bool
}

// RenderOutputTemplate renders common yt-dlp-style output template tokens.
func RenderOutputTemplate(template string, data OutputTemplateData) string {
	sanitize := SanitizeOutputTemplateToken
	if data.RestrictFilenames {
		sanitize = SanitizeRestrictedOutputTemplateToken
	}
	values := map[string]string{
		"%(id)s":           sanitize(data.VideoID),
		"%(title)s":        sanitize(data.Title),
		"%(uploader)s":     sanitize(data.Uploader),
		"%(uploader_id)s":  sanitize(data.UploaderID),
		"%(channel)s":      sanitize(data.Channel),
		"%(channel_id)s":   sanitize(data.ChannelID),
		"%(ext)s":          sanitize(data.Ext),
		"%(itag)s":         sanitize(data.Itag),
		"%(format_id)s":    sanitize(firstNonEmpty(data.FormatID, data.Itag)),
		"%(resolution)s":   sanitize(data.Resolution),
		"%(width)s":        sanitize(data.Width),
		"%(height)s":       sanitize(data.Height),
		"%(fps)s":          sanitize(data.FPS),
		"%(tbr)s":          sanitize(data.TBR),
		"%(vbr)s":          sanitize(data.VBR),
		"%(abr)s":          sanitize(data.ABR),
		"%(protocol)s":     sanitize(data.Protocol),
		"%(vcodec)s":       sanitize(data.VCodec),
		"%(acodec)s":       sanitize(data.ACodec),
		"%(upload_date)s":  sanitize(data.UploadDate),
		"%(release_date)s": sanitize(data.ReleaseDate),
		"%(timestamp)s":    sanitize(data.Timestamp),
	}
	pairs := make([]string, 0, 2*len(values))
	for token, value := range values {
		pairs = append(pairs, token, value)
	}
	return strings.NewReplacer(pairs...).Replace(template)
}

// FormatTemplateTokens contains template token values derived from selected formats.
type FormatTemplateTokens struct {
	Resolution string
	Width      string
	Height     string
	FPS        string
	TBR        string
	VBR        string
	ABR        string
	Protocol   string
	VCodec     string
	ACodec     string
}

// SelectedFormatTemplateTokens derives output-template tokens from selected formats.
func SelectedFormatTemplateTokens(formats []FormatInfo) FormatTemplateTokens {
	out := FormatTemplateTokens{
		TBR:      formatBitrateKbps(totalSelectedBitrate(formats)),
		Protocol: selectedProtocolToken(formats),
	}
	for _, format := range formats {
		if format.HasVideo && format.Bitrate > 0 && out.VBR == "" {
			out.VBR = formatBitrateKbps(format.Bitrate)
		}
		if format.HasAudio && format.Bitrate > 0 && out.ABR == "" {
			out.ABR = formatBitrateKbps(format.Bitrate)
		}
		if format.HasVideo && out.VCodec == "" {
			out.VCodec = codecFromFormat(format, true)
		}
		if format.HasAudio && out.ACodec == "" {
			out.ACodec = codecFromFormat(format, false)
		}
	}
	if out.VCodec == "" {
		out.VCodec = "none"
	}
	if out.ACodec == "" {
		out.ACodec = "none"
	}
	format, ok := selectedVideoFormatForTemplate(formats)
	if !ok {
		out.Resolution = "audio only"
		return out
	}
	out.Resolution = fmt.Sprintf("%dx%d", format.Width, format.Height)
	if format.Width > 0 {
		out.Width = strconv.Itoa(format.Width)
	}
	if format.Height > 0 {
		out.Height = strconv.Itoa(format.Height)
	}
	if format.FPS > 0 {
		out.FPS = strconv.Itoa(format.FPS)
	}
	return out
}

// SanitizeOutputTemplateToken normalizes a token value for filesystem paths.
func SanitizeOutputTemplateToken(v string) string {
	return sanitizeOutputToken(v)
}

// SanitizeRestrictedOutputTemplateToken normalizes a token value for ASCII-safe filenames.
func SanitizeRestrictedOutputTemplateToken(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	b.Grow(len(v))
	lastUnderscore := false
	for _, r := range v {
		keep := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '-'
		if keep {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_.")
	if out == "" {
		return "unknown"
	}
	return out
}

func selectedProtocolToken(formats []FormatInfo) string {
	parts := make([]string, 0, len(formats))
	seen := make(map[string]struct{}, len(formats))
	for _, format := range formats {
		protocol := strings.TrimSpace(format.Protocol)
		if protocol == "" {
			continue
		}
		if _, ok := seen[protocol]; ok {
			continue
		}
		seen[protocol] = struct{}{}
		parts = append(parts, protocol)
	}
	return strings.Join(parts, "+")
}

func codecFromFormat(format FormatInfo, video bool) string {
	_, params, err := mime.ParseMediaType(format.MimeType)
	if err != nil {
		return ""
	}
	codecs := parseCodecsParam(params["codecs"])
	if len(codecs) == 0 {
		return ""
	}
	if video {
		for _, codec := range codecs {
			if isLikelyVideoCodec(codec) {
				return codec
			}
		}
		if format.HasVideo && !format.HasAudio {
			return codecs[0]
		}
		return ""
	}
	for _, codec := range codecs {
		if !isLikelyVideoCodec(codec) {
			return codec
		}
	}
	if format.HasAudio && !format.HasVideo {
		return codecs[0]
	}
	return ""
}

func parseCodecsParam(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		codec := strings.Trim(strings.TrimSpace(part), `"`)
		if codec != "" {
			out = append(out, codec)
		}
	}
	return out
}

func isLikelyVideoCodec(codec string) bool {
	codec = strings.ToLower(strings.TrimSpace(codec))
	return strings.HasPrefix(codec, "avc") ||
		strings.HasPrefix(codec, "hev") ||
		strings.HasPrefix(codec, "hvc") ||
		strings.HasPrefix(codec, "vp") ||
		strings.HasPrefix(codec, "av01") ||
		strings.HasPrefix(codec, "theora")
}

func totalSelectedBitrate(formats []FormatInfo) int {
	total := 0
	for _, format := range formats {
		if format.Bitrate > 0 {
			total += format.Bitrate
		}
	}
	return total
}

func formatBitrateKbps(bitrate int) string {
	if bitrate <= 0 {
		return ""
	}
	return strconv.Itoa(bitrate / 1000)
}

func selectedVideoFormatForTemplate(formats []FormatInfo) (FormatInfo, bool) {
	for _, format := range formats {
		if format.HasVideo && !format.HasAudio {
			return format, true
		}
	}
	for _, format := range formats {
		if format.HasVideo {
			return format, true
		}
	}
	return FormatInfo{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
