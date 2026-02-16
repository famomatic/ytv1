package selector

import (
	"mime"
	"sort"
	"strconv"
	"strings"

	"github.com/famomatic/ytv1/internal/types"
)

// Select chooses the best formats based on the selector.
func Select(formats []types.FormatInfo, selector *Selector) ([]types.FormatInfo, error) {
	if selector == nil || len(selector.Fallbacks) == 0 {
		return SelectBest(formats), nil
	}

	for _, group := range selector.Fallbacks {
		// A MergeGroup is a list of StreamSpecs (e.g. [video, audio])
		var selected []types.FormatInfo
		failed := false

		for _, spec := range group {
			candidate, ok := pickBest(formats, spec)
			if !ok {
				failed = true
				break
			}
			selected = append(selected, candidate)
		}

		if !failed {
			return selected, nil
		}
	}

	return nil, nil
}

// SelectBest implements the default 'best' logic.
func SelectBest(formats []types.FormatInfo) []types.FormatInfo {
	// 1. Prefer formats with both Audio and Video
	var av []types.FormatInfo
	for _, f := range formats {
		if f.HasAudio && f.HasVideo {
			av = append(av, f)
		}
	}

	if len(av) > 0 {
		sortFormats(av)
		return []types.FormatInfo{av[0]}
	}

	// 2. Fallback: Return best format available
	if len(formats) > 0 {
		sorted := make([]types.FormatInfo, len(formats))
		copy(sorted, formats)
		sortFormats(sorted)
		return []types.FormatInfo{sorted[0]}
	}

	return nil
}

func pickBest(formats []types.FormatInfo, spec *StreamSpec) (types.FormatInfo, bool) {
	var candidates []types.FormatInfo

	// Filter candidates that match ALL filters in spec
	for _, f := range formats {
		if matchesAll(f, spec.Filters) {
			candidates = append(candidates, f)
		}
	}

	if len(candidates) == 0 {
		return types.FormatInfo{}, false
	}

	sortFormats(candidates)

	// If this spec requests a worst variant (builtin or media-specific),
	// pick the tail after ranking.
	if wantsWorst(spec.Filters) {
		return candidates[len(candidates)-1], true
	}

	return candidates[0], true
}

func wantsWorst(filters []FormatFilter) bool {
	for _, flt := range filters {
		if flt.Type == "builtin" && flt.Value == "worst" {
			return true
		}
		if flt.Type == "media" && flt.Op == "worst" {
			return true
		}
	}
	return false
}

func matchesAll(f types.FormatInfo, filters []FormatFilter) bool {
	for _, flt := range filters {
		if !matches(f, &flt) {
			return false
		}
	}
	return true
}

func matches(f types.FormatInfo, filter *FormatFilter) bool {
	switch filter.Type {
	case "builtin":
		return true
	case "media":
		if filter.Value == "video" {
			return f.HasVideo && !f.HasAudio
		}
		if filter.Value == "audio" {
			return f.HasAudio && !f.HasVideo
		}
	case "ext":
		return formatExt(f) == strings.ToLower(filter.Value)
	case "res":
		val, err := strconv.Atoi(filter.Value)
		if err != nil {
			return false
		}
		return checkOp(f.Height, val, filter.Op)
	case "width":
		val, err := strconv.Atoi(filter.Value)
		if err != nil {
			return false
		}
		return checkOp(f.Width, val, filter.Op)
	case "fps":
		val, err := strconv.Atoi(filter.Value)
		if err != nil {
			return false
		}
		return checkOp(f.FPS, val, filter.Op)
	}
	return false
}

func checkOp(a, b int, op string) bool {
	switch op {
	case ":", "=":
		return a == b
	case "<":
		return a < b
	case ">":
		return a > b
	case "<=":
		return a <= b
	case ">=":
		return a >= b
	case "!=":
		return a != b
	}
	return false
}

func sortFormats(formats []types.FormatInfo) {
	sort.Slice(formats, func(i, j int) bool {
		if trackRank(formats[i]) != trackRank(formats[j]) {
			return trackRank(formats[i]) > trackRank(formats[j])
		}
		// Descending order
		resI := formats[i].Height * formats[i].Width
		resJ := formats[j].Height * formats[j].Width
		if resI != resJ {
			return resI > resJ
		}
		if formats[i].Bitrate != formats[j].Bitrate {
			return formats[i].Bitrate > formats[j].Bitrate
		}
		if formats[i].FPS != formats[j].FPS {
			return formats[i].FPS > formats[j].FPS
		}
		return formats[i].Itag > formats[j].Itag
	})
}

func trackRank(f types.FormatInfo) int {
	switch {
	case f.HasAudio && f.HasVideo:
		return 3
	case f.HasVideo:
		return 2
	case f.HasAudio:
		return 1
	default:
		return 0
	}
}

func formatExt(f types.FormatInfo) string {
	mediaType, _, err := mime.ParseMediaType(f.MimeType)
	if err != nil {
		return ""
	}
	parts := strings.SplitN(strings.ToLower(mediaType), "/", 2)
	if len(parts) != 2 {
		return ""
	}
	subtype := parts[1]
	if f.HasAudio && !f.HasVideo && subtype == "mp4" {
		return "m4a"
	}
	return subtype
}
