package formats

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// HLSManifest represents a parsed HLS manifest.
type HLSManifest struct {
	RawContent string
	Formats    []Format
}

func FetchHLSManifest(ctx context.Context, client *http.Client, url string) (*HLSManifest, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch HLS manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}

	raw := string(body)
	parsedFormats, _ := ParseHLSManifest(raw, url)

	return &HLSManifest{
		RawContent: raw,
		Formats:    parsedFormats,
	}, nil
}

// ParseHLSManifest parses an HLS master playlist into normalized formats.
func ParseHLSManifest(raw, manifestURL string) ([]Format, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
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
			pendingStreamAttrs = parseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			attrs := parseM3U8Attrs(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
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
		formats = append(formats, f)
		pendingStreamAttrs = nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return formats, nil
}

func parseM3U8Attrs(raw string) map[string]string {
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
	return int(v)
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

func extractCodecsFromMime(mimeType string) []string {
	_, codecs := parseMimeDetails(mimeType)
	return codecs
}
