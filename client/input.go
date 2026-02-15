package client

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

var (
	youtubeIDPattern   = regexp.MustCompile(`^[0-9A-Za-z_-]{11}$`)
	watchURLPattern    = regexp.MustCompile(`(?:v=|/shorts/|youtu\.be/)([0-9A-Za-z_-]{11})`)
	playlistIDPattern  = regexp.MustCompile(`^(PL|UU|LL|RD|OLAK5uy_)[0-9A-Za-z_-]+$`)
	playlistURLPattern = regexp.MustCompile(`(?:[?&]list=)([0-9A-Za-z_-]+)`)
)

// ExtractVideoID accepts either a raw id or common YouTube URL shapes.
func ExtractVideoID(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", invalidInput(input, "empty_input")
	}
	if youtubeIDPattern.MatchString(s) {
		return s, nil
	}

	if parsed, ok := tryParseURL(s); ok {
		if !isYouTubeHost(parsed.Hostname()) {
			return "", invalidInput(input, "unsupported_host")
		}
		if id := extractVideoIDFromURL(parsed); id != "" {
			if youtubeIDPattern.MatchString(id) {
				return id, nil
			}
			return "", invalidInput(input, "invalid_video_id")
		}
		return "", invalidInput(input, "missing_video_id")
	}

	m := watchURLPattern.FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1], nil
	}
	return "", invalidInput(input, "unsupported_input_shape")
}

// ExtractPlaylistID accepts raw playlist IDs or common YouTube playlist URL shapes.
func ExtractPlaylistID(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", invalidInput(input, "empty_input")
	}
	if playlistIDPattern.MatchString(s) {
		return s, nil
	}

	if parsed, ok := tryParseURL(s); ok {
		if !isYouTubeHost(parsed.Hostname()) {
			return "", invalidInput(input, "unsupported_host")
		}
		if listID := strings.TrimSpace(parsed.Query().Get("list")); listID != "" {
			return listID, nil
		}
		return "", invalidInput(input, "missing_playlist_id")
	}

	m := playlistURLPattern.FindStringSubmatch(s)
	if len(m) == 2 && m[1] != "" {
		return m[1], nil
	}
	return "", invalidInput(input, "unsupported_input_shape")
}

func invalidInput(input, reason string) error {
	return &InvalidInputDetailError{
		Input:  strings.TrimSpace(input),
		Reason: reason,
	}
}

func tryParseURL(s string) (*url.URL, bool) {
	if !strings.Contains(s, "://") && strings.Contains(s, ".") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u == nil || u.Host == "" {
		return nil, false
	}
	return u, true
}

func isYouTubeHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimPrefix(h, "www.")
	return h == "youtube.com" || h == "m.youtube.com" || h == "music.youtube.com" || h == "youtu.be"
}

func extractVideoIDFromURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	switch host {
	case "youtu.be":
		id := strings.Trim(path.Clean(u.Path), "/")
		return firstPathSegment(id)
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		if v := strings.TrimSpace(u.Query().Get("v")); v != "" {
			return v
		}
		p := strings.Trim(path.Clean(u.Path), "/")
		parts := strings.Split(p, "/")
		if len(parts) < 2 {
			return ""
		}
		switch parts[0] {
		case "embed", "v", "shorts", "live":
			return parts[1]
		default:
			return ""
		}
	default:
		return ""
	}
}

func firstPathSegment(p string) string {
	if p == "" {
		return ""
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}
