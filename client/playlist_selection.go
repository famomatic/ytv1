package client

import (
	"fmt"
	"strconv"
	"strings"
)

// SelectPlaylistItems applies a yt-dlp-style playlist item selector to items.
//
// Selectors use 1-based indexes and support comma-separated values, open ranges,
// negative indexes, and positive/negative steps such as "1,3:5,-3:-1,::-1".
func SelectPlaylistItems(items []PlaylistItem, selector string) ([]PlaylistItem, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return items, nil
	}
	selected := make([]bool, len(items))
	selectedOrder := make([]int, 0, len(items))
	for _, part := range strings.Split(selector, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("%w: empty playlist item selector in %q", ErrInvalidInput, selector)
		}
		start, end, step, err := parsePlaylistItemRange(part, len(items))
		if err != nil {
			return nil, err
		}
		for _, i := range playlistRangeIndexes(start, end, step) {
			if i < 1 || i > len(items) || selected[i-1] {
				continue
			}
			selected[i-1] = true
			selectedOrder = append(selectedOrder, i)
		}
	}
	out := make([]PlaylistItem, 0, len(selectedOrder))
	for _, idx := range selectedOrder {
		out = append(out, items[idx-1])
	}
	return out, nil
}

func playlistRangeIndexes(start int, end int, step int) []int {
	if step == 0 {
		return nil
	}
	out := make([]int, 0)
	if step > 0 {
		for i := start; i <= end; i += step {
			out = append(out, i)
			if step > end-i {
				break
			}
		}
		return out
	}
	for i := start; i >= end; i += step {
		out = append(out, i)
		if step < end-i {
			break
		}
	}
	return out
}

func parsePlaylistItemRange(part string, total int) (int, int, int, error) {
	if total < 0 {
		total = 0
	}
	if strings.Contains(part, "-") && !strings.Contains(part, ":") && !strings.HasPrefix(strings.TrimSpace(part), "-") {
		bounds := strings.Split(part, "-")
		if len(bounds) != 2 || strings.TrimSpace(bounds[0]) == "" || strings.TrimSpace(bounds[1]) == "" {
			return 0, 0, 0, fmt.Errorf("%w: invalid playlist item range %q", ErrInvalidInput, part)
		}
		start, err := parsePositivePlaylistIndex(bounds[0], part)
		if err != nil {
			return 0, 0, 0, err
		}
		end, err := parsePositivePlaylistIndex(bounds[1], part)
		if err != nil {
			return 0, 0, 0, err
		}
		return clampPlaylistRange(start, end, 1, total, part)
	}
	if strings.Contains(part, ":") {
		bounds := strings.Split(part, ":")
		if len(bounds) < 2 || len(bounds) > 3 {
			return 0, 0, 0, fmt.Errorf("%w: invalid playlist item range %q", ErrInvalidInput, part)
		}
		start := 1
		end := total
		step := 1
		var err error
		if len(bounds) == 3 {
			step, err = parsePlaylistStep(bounds[2], part)
			if err != nil {
				return 0, 0, 0, err
			}
			if step < 0 {
				start = total
				end = 1
			}
		}
		if strings.TrimSpace(bounds[0]) != "" {
			start, err = parsePlaylistIndex(bounds[0], total, part)
			if err != nil {
				return 0, 0, 0, err
			}
		}
		if strings.TrimSpace(bounds[1]) != "" {
			end, err = parsePlaylistIndex(bounds[1], total, part)
			if err != nil {
				return 0, 0, 0, err
			}
		}
		return clampPlaylistRange(start, end, step, total, part)
	}
	idx, err := parsePlaylistIndex(part, total, part)
	if err != nil {
		return 0, 0, 0, err
	}
	if idx < 1 || idx > total {
		return 1, 0, 1, nil
	}
	return idx, idx, 1, nil
}

func clampPlaylistRange(start int, end int, step int, total int, context string) (int, int, int, error) {
	if step == 0 {
		return 0, 0, 0, fmt.Errorf("%w: invalid playlist item step %q", ErrInvalidInput, context)
	}
	if step > 0 && start > end {
		return 0, 0, 0, fmt.Errorf("%w: invalid descending playlist item range %q", ErrInvalidInput, context)
	}
	if step < 0 && start < end {
		return 0, 0, 0, fmt.Errorf("%w: invalid ascending playlist item range for negative step %q", ErrInvalidInput, context)
	}
	if total == 0 {
		return 1, 0, 1, nil
	}
	if step > 0 {
		if start > total || end < 1 {
			return 1, 0, 1, nil
		}
		if start < 1 {
			start = 1
		}
		if end > total {
			end = total
		}
		return start, end, step, nil
	}
	if start < 1 || end > total {
		return 1, 0, 1, nil
	}
	if start > total {
		start = total
	}
	if end < 1 {
		end = 1
	}
	return start, end, step, nil
}

func parsePositivePlaylistIndex(raw string, context string) (int, error) {
	idx, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || idx < 1 {
		return 0, fmt.Errorf("%w: invalid playlist item selector %q", ErrInvalidInput, context)
	}
	return idx, nil
}

func parsePlaylistIndex(raw string, total int, context string) (int, error) {
	idx, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || idx == 0 {
		return 0, fmt.Errorf("%w: invalid playlist item selector %q", ErrInvalidInput, context)
	}
	if idx < 0 {
		return total + idx + 1, nil
	}
	return idx, nil
}

func parsePlaylistStep(raw string, context string) (int, error) {
	step, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || step == 0 {
		return 0, fmt.Errorf("%w: invalid playlist item step %q", ErrInvalidInput, context)
	}
	return step, nil
}
