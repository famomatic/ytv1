package formats

import (
	"sort"
)

// SortByBest sorts formats by Resolution -> Bitrate -> FPS.
func SortByBest(formats []Format) {
	sort.Slice(formats, func(i, j int) bool {
		// 1. Resolution (Height)
		if formats[i].Height != formats[j].Height {
			return formats[i].Height > formats[j].Height
		}
		// 2. Bitrate (AverageBitrate or Bitrate)
		bitrateI := formats[i].AverageBitrate
		if bitrateI == 0 {
			bitrateI = formats[i].Bitrate
		}
		bitrateJ := formats[j].AverageBitrate
		if bitrateJ == 0 {
			bitrateJ = formats[j].Bitrate
		}
		if bitrateI != bitrateJ {
			return bitrateI > bitrateJ
		}
		return false
	})
}
