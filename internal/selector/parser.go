package selector

import (
	"fmt"
	"regexp"
	"strings"
)

// Selector represents a parsed format selection strategy.
type Selector struct {
	// Fallbacks: Each element is a Merge Group.
	// We try the first Merge Group. If it fails, try the next.
	Fallbacks []MergeGroup
}

// MergeGroup is a list of StreamSpecs to be downloaded and merged.
// E.g. "bestvideo+bestaudio" -> [StreamSpec(video), StreamSpec(audio)]
type MergeGroup []*StreamSpec

// StreamSpec defines criteria for ONE stream.
// It can have multiple filters (e.g. bestvideo AND ext=mp4).
type StreamSpec struct {
	Filters []FormatFilter
}

// FormatFilter represents a single criteria (e.g., bestvideo, res:1080).
type FormatFilter struct {
	Type  string // best, worst, video, audio, extension
	Value string // 1080, mp4, etc.
	Op    string // =, <, >, <=, >= (for filters like res)
}

// Parse parses a format selector string.
// Syntax: seg1+seg2/seg3
// Modifier syntax: bestvideo[ext=mp4]
func Parse(s string) (*Selector, error) {
	// Splits by / first (fallbacks)
	fallbackStrs := strings.Split(s, "/")
	var fallbacks []MergeGroup

	for _, fbStr := range fallbackStrs {
		// Splits by + (merge groups)
		mergeStrs := strings.Split(fbStr, "+")
		var group MergeGroup
		for _, mStr := range mergeStrs {
			spec, err := parseStreamSpec(strings.TrimSpace(mStr))
			if err != nil {
				return nil, err
			}
			group = append(group, spec)
		}
		fallbacks = append(fallbacks, group)
	}

	return &Selector{Fallbacks: fallbacks}, nil
}

var resRegex = regexp.MustCompile(`^(res|height|width)(:|<=|>=|=|<|>)(\d+)$`)

func parseStreamSpec(s string) (*StreamSpec, error) {
	// s = "bestvideo[ext=mp4]"
	// Split into base "bestvideo" and modifiers "[ext=mp4]"

	// Simple parsing: Find first '['
	idx := strings.Index(s, "[")
	var base string
	var mods string
	if idx == -1 {
		base = s
	} else {
		base = s[:idx]
		mods = s[idx:]
	}

	spec := &StreamSpec{}

	// Parse base
	if base != "" {
		f, err := parseFilter(base)
		if err != nil {
			return nil, err
		}
		spec.Filters = append(spec.Filters, *f)
	}

	// Parse modifiers
	modRex := regexp.MustCompile(`\[([^\]]+)\]`)
	matches := modRex.FindAllStringSubmatch(mods, -1)
	for _, m := range matches {
		// m[1] is the content "ext=mp4"
		inner := m[1]
		f, err := parseModifier(inner)
		if err != nil {
			return nil, err
		}
		spec.Filters = append(spec.Filters, *f)
	}

	return spec, nil
}

func parseModifier(s string) (*FormatFilter, error) {
	// s = "ext=mp4" or "height<720"
	// Check ops
	ops := []string{"<=", ">=", "!=", "=", "<", ">", ":"}
	for _, op := range ops {
		if idx := strings.Index(s, op); idx != -1 {
			key := strings.TrimSpace(s[:idx])
			val := strings.TrimSpace(s[idx+len(op):])

			// Map key to filter type
			switch key {
			case "ext":
				return &FormatFilter{Type: "ext", Value: val}, nil
			case "res", "height":
				return &FormatFilter{Type: "res", Value: val, Op: op}, nil
			case "width":
				return &FormatFilter{Type: "width", Value: val, Op: op}, nil
			case "fps":
				return &FormatFilter{Type: "fps", Value: val, Op: op}, nil
			default:
				// unknown key, maybe metadata? ignore or error?
				// yt-dlp allows metadata matches.
				return nil, fmt.Errorf("unknown modifier key: %s", key)
			}
		}
	}
	return nil, fmt.Errorf("unknown modifier syntax: %s", s)
}

func parseFilter(s string) (*FormatFilter, error) {
	s = strings.ToLower(s)

	if s == "best" || s == "worst" {
		return &FormatFilter{Type: "builtin", Value: s}, nil
	}
	if s == "bestvideo" || s == "bv" {
		return &FormatFilter{Type: "media", Value: "video", Op: "best"}, nil
	}
	if s == "worstvideo" || s == "wv" {
		return &FormatFilter{Type: "media", Value: "video", Op: "worst"}, nil
	}
	if s == "bestaudio" || s == "ba" {
		return &FormatFilter{Type: "media", Value: "audio", Op: "best"}, nil
	}
	if s == "worstaudio" || s == "wa" {
		return &FormatFilter{Type: "media", Value: "audio", Op: "worst"}, nil
	}
	if s == "videoonly" {
		return &FormatFilter{Type: "media", Value: "video"}, nil
	}
	if s == "audioonly" {
		return &FormatFilter{Type: "media", Value: "audio"}, nil
	}

	// Extension shortcuts (mp4, webm)
	if s == "mp4" || s == "webm" || s == "m4a" || s == "mp3" {
		return &FormatFilter{Type: "ext", Value: s}, nil
	}

	// Resolution shortcut (res:1080)
	if matches := resRegex.FindStringSubmatch(s); matches != nil {
		filterType := "res"
		if matches[1] == "width" {
			filterType = "width"
		}
		return &FormatFilter{
			Type:  filterType,
			Value: matches[3],
			Op:    matches[2],
		}, nil
	}

	// Allow standalone modifier-style filters as base tokens, e.g.:
	// "fps!=60", "ext=mp4", "height<=720"
	if flt, err := parseModifier(s); err == nil {
		return flt, nil
	}

	return nil, fmt.Errorf("unknown selector: %s", s)
}
