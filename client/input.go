package client

import (
	"regexp"
	"strings"
)

var (
	youtubeIDPattern = regexp.MustCompile(`^[0-9A-Za-z_-]{11}$`)
	watchURLPattern  = regexp.MustCompile(`(?:v=|/shorts/|youtu\.be/)([0-9A-Za-z_-]{11})`)
)

// ExtractVideoID accepts either a raw id or common YouTube URL shapes.
func ExtractVideoID(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", ErrInvalidInput
	}
	if youtubeIDPattern.MatchString(s) {
		return s, nil
	}
	m := watchURLPattern.FindStringSubmatch(s)
	if len(m) == 2 {
		return m[1], nil
	}
	return "", ErrInvalidInput
}
