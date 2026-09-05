package formats

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/famomatic/ytv1/internal/iox"
)

// HLSManifest represents a parsed HLS manifest.
type HLSManifest struct {
	RawContent string
	Formats    []Format
}

func FetchHLSManifest(ctx context.Context, client *http.Client, url string, headers ...http.Header) (*HLSManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	for _, h := range headers {
		for key, values := range h {
			req.Header[key] = append([]string(nil), values...)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch HLS manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := iox.ReadAllLimit(resp.Body, 5<<20) // 5 MB HLS manifest limit
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	raw := string(body)
	parsedFormats, parseErr := ParseHLSManifest(raw, url)
	if parseErr != nil {
		return nil, parseErr
	}

	return &HLSManifest{
		RawContent: raw,
		Formats:    parsedFormats,
	}, nil
}

// ParseHLSManifest parses an HLS master playlist into normalized formats.
func ParseHLSManifest(raw, manifestURL string) ([]Format, error) {
	if !strings.HasPrefix(strings.TrimSpace(raw), "#EXTM3U") {
		return nil, fmt.Errorf("invalid HLS manifest header")
	}

	// EXT-X-STREAM-INF may precede or follow the EXT-X-MEDIA declaration it
	// references. Collect URI-backed audio groups first so a variant whose
	// CODECS attribute lists both its video codec and the external rendition's
	// audio codec is not mistaken for an internally muxed AV stream.
	audioGroups := make(map[string]bool)
	groupScanner := bufio.NewScanner(strings.NewReader(raw))
	for groupScanner.Scan() {
		line := strings.TrimSpace(groupScanner.Text())
		if !strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			continue
		}
		attrs := ParseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
		if strings.EqualFold(attrs["TYPE"], "AUDIO") && strings.TrimSpace(attrs["URI"]) != "" {
			audioGroups[attrs["GROUP-ID"]] = true
		}
	}
	if err := groupScanner.Err(); err != nil {
		return nil, err
	}

	formats := make([]Format, 0, 16)
	scanner := bufio.NewScanner(strings.NewReader(raw))
	var pendingStreamAttrs map[string]string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			pendingStreamAttrs = ParseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			attrs := ParseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
			if !strings.EqualFold(attrs["TYPE"], "AUDIO") {
				continue
			}
			uri := strings.TrimSpace(attrs["URI"])
			if uri == "" {
				continue
			}
			u := resolveM3U8RefURL(manifestURL, uri)
			f := Format{
				Itag:     inferItagFromURL(u),
				URL:      u,
				MimeType: inferMimeFromM3U8Codecs(attrs["CODECS"]),
				Bitrate:  parseInt(attrs["BANDWIDTH"]),
				Protocol: "hls",
			}
			// YouTube's demuxed audio renditions often omit CODECS; without
			// this override the mime fallback ("video/mp4") would classify the
			// audio track as a 0x0 video-only format.
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(f.MimeType)), "audio/") {
				f.MimeType = "audio/mp4"
			}
			if channels := parseInt(attrs["CHANNELS"]); channels > 0 {
				f.AudioChannels = channels
			}
			f.HasAudio, f.HasVideo = deriveMediaFlags(f, true)
			formats = append(formats, f)
			continue
		}

		// URI line for the immediately preceding EXT-X-STREAM-INF.
		if strings.HasPrefix(line, "#") {
			continue
		}
		if pendingStreamAttrs == nil {
			continue
		}
		uri := resolveM3U8RefURL(manifestURL, line)
		resW, resH := parseM3U8Resolution(pendingStreamAttrs["RESOLUTION"])
		mimeType := inferMimeFromM3U8Codecs(pendingStreamAttrs["CODECS"])
		f := Format{
			Itag:      inferItagFromURL(uri),
			URL:       uri,
			MimeType:  mimeType,
			Bitrate:   parseInt(pendingStreamAttrs["AVERAGE-BANDWIDTH"]),
			Width:     resW,
			Height:    resH,
			FPS:       parseFloatToInt(pendingStreamAttrs["FRAME-RATE"]),
			Protocol:  "hls",
			Container: "mp4",
		}
		if f.Bitrate == 0 {
			f.Bitrate = parseInt(pendingStreamAttrs["BANDWIDTH"])
		}
		if codecs := extractCodecsFromMime(mimeType); len(codecs) > 0 {
			f.Codecs = codecs
		}
		f.HasAudio, f.HasVideo = deriveMediaFlags(f, true)
		codecsRaw := strings.TrimSpace(pendingStreamAttrs["CODECS"])
		audioGroup := strings.TrimSpace(pendingStreamAttrs["AUDIO"])
		if codecsRaw != "" && audioGroups[audioGroup] && f.HasVideo {
			// RFC 8216 requires CODECS to describe codecs used by referenced
			// rendition groups as well as the variant itself. YouTube therefore
			// lists mp4a here even though the selected itag contains video only;
			// the AAC packets live in the URI-backed EXT-X-MEDIA rendition.
			videoCodecs := filterM3U8Codecs(codecsRaw, true)
			f.MimeType = inferMimeFromM3U8Codecs(videoCodecs)
			f.Codecs = extractCodecsFromMime(f.MimeType)
			f.HasAudio, f.HasVideo = false, true
		} else if codecsRaw == "" {
			// A master-playlist variant with no CODECS attribute is a muxed
			// audio+video rendition by HLS convention. Without this, the
			// inferred "video/mp4" mime marks it video-only and it is wrongly
			// selected as needing a separate audio stream.
			f.HasAudio, f.HasVideo = true, true
		}
		formats = append(formats, f)
		pendingStreamAttrs = nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return formats, nil
}

// ParseM3U8Attrs parses M3U8 attribute lists (KEY=VALUE,...).
func ParseM3U8Attrs(raw string) map[string]string {
	out := map[string]string{}
	rest := raw
	for len(rest) > 0 {
		eq := strings.IndexByte(rest, '=')
		if eq <= 0 {
			break
		}
		key := strings.TrimSpace(rest[:eq])
		rest = rest[eq+1:]
		if len(rest) == 0 {
			break
		}
		var value string
		if rest[0] == '"' {
			rest = rest[1:]
			end := strings.IndexByte(rest, '"')
			if end < 0 {
				value = rest
				rest = ""
			} else {
				value = rest[:end]
				rest = rest[end+1:]
			}
		} else {
			comma := strings.IndexByte(rest, ',')
			if comma < 0 {
				value = rest
				rest = ""
			} else {
				value = rest[:comma]
				rest = rest[comma+1:]
			}
		}
		out[strings.ToUpper(strings.TrimSpace(key))] = strings.TrimSpace(value)
		if len(rest) > 0 && rest[0] == ',' {
			rest = rest[1:]
		}
		rest = strings.TrimLeft(rest, " ")
	}
	return out
}

func parseM3U8Resolution(raw string) (int, int) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, 0
	}
	parts := strings.SplitN(s, "x", 2)
	if len(parts) != 2 {
		return 0, 0
	}
	return parseInt(parts[0]), parseInt(parts[1])
}

func parseFloatToInt(raw string) int {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	// Round, not truncate: HLS FRAME-RATE is a decimal (e.g. 59.94) and must
	// map to 60 so `fps=` selectors match.
	return int(math.Round(v))
}

func resolveM3U8RefURL(manifestURL, ref string) string {
	ref = strings.Trim(strings.TrimSpace(ref), `"`)
	base, err := url.Parse(manifestURL)
	if err != nil {
		return ref
	}
	out, err := base.Parse(ref)
	if err != nil {
		return ref
	}
	return out.String()
}

func inferItagFromURL(rawURL string) int {
	u, err := url.Parse(rawURL)
	if err == nil {
		if itag := parseInt(u.Query().Get("itag")); itag > 0 {
			return itag
		}
		parts := strings.Split(u.Path, "/")
		for i, p := range parts {
			if p == "itag" && i+1 < len(parts) {
				if itag := parseInt(parts[i+1]); itag > 0 {
					return itag
				}
			}
		}
	}
	return 0
}

func inferMimeFromM3U8Codecs(codecsRaw string) string {
	codecs := strings.TrimSpace(codecsRaw)
	if codecs == "" {
		return "video/mp4"
	}
	lc := strings.ToLower(codecs)
	hasVideo := strings.Contains(lc, "avc1") || strings.Contains(lc, "av01") || strings.Contains(lc, "vp9") || strings.Contains(lc, "hev1") || strings.Contains(lc, "hvc1")
	hasAudio := strings.Contains(lc, "mp4a") || strings.Contains(lc, "opus") || strings.Contains(lc, "aac")
	switch {
	case hasVideo && hasAudio:
		return `video/mp4; codecs="` + codecs + `"`
	case hasVideo:
		return `video/mp4; codecs="` + codecs + `"`
	case hasAudio:
		return `audio/mp4; codecs="` + codecs + `"`
	default:
		return "video/mp4"
	}
}

func filterM3U8Codecs(codecsRaw string, video bool) string {
	filtered := make([]string, 0, 2)
	for _, codec := range strings.Split(codecsRaw, ",") {
		codec = strings.TrimSpace(codec)
		lc := strings.ToLower(codec)
		isVideo := strings.HasPrefix(lc, "avc1") || strings.HasPrefix(lc, "av01") ||
			strings.HasPrefix(lc, "vp9") || strings.HasPrefix(lc, "vp09") ||
			strings.HasPrefix(lc, "hev1") || strings.HasPrefix(lc, "hvc1")
		isAudio := strings.HasPrefix(lc, "mp4a") || strings.HasPrefix(lc, "opus") ||
			strings.HasPrefix(lc, "aac")
		if (video && isVideo) || (!video && isAudio) {
			filtered = append(filtered, codec)
		}
	}
	return strings.Join(filtered, ",")
}

func extractCodecsFromMime(mimeType string) []string {
	_, codecs := parseMimeDetails(mimeType)
	return codecs
}
