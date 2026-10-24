package innertube

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AdPlacement mirrors the parts of `adPlacements[]` needed to find a start ad.
type AdPlacement struct {
	AdPlacementRenderer struct {
		Config struct {
			AdPlacementConfig struct {
				Kind string `json:"kind"`
			} `json:"adPlacementConfig"`
		} `json:"config"`
		Renderer struct {
			InstreamVideoAdRenderer *InstreamVideoAdRenderer `json:"instreamVideoAdRenderer"`
		} `json:"renderer"`
	} `json:"adPlacementRenderer"`
}

// AdSlot mirrors the parts of `adSlots[]` needed to find a before-content ad.
type AdSlot struct {
	AdSlotRenderer struct {
		AdSlotMetadata struct {
			TriggerEvent string `json:"triggerEvent"`
		} `json:"adSlotMetadata"`
		FulfillmentContent struct {
			FulfilledLayout struct {
				PlayerBytesAdLayoutRenderer adLayoutRenderer `json:"playerBytesAdLayoutRenderer"`
			} `json:"fulfilledLayout"`
		} `json:"fulfillmentContent"`
	} `json:"adSlotRenderer"`
}

type adLayoutRenderer struct {
	RenderingContent struct {
		InstreamVideoAdRenderer             *InstreamVideoAdRenderer `json:"instreamVideoAdRenderer"`
		PlayerBytesSequentialLayoutRenderer struct {
			SequentialLayouts []struct {
				PlayerBytesAdLayoutRenderer struct {
					RenderingContent struct {
						InstreamVideoAdRenderer *InstreamVideoAdRenderer `json:"instreamVideoAdRenderer"`
					} `json:"renderingContent"`
				} `json:"playerBytesAdLayoutRenderer"`
			} `json:"sequentialLayouts"`
		} `json:"playerBytesSequentialLayoutRenderer"`
	} `json:"renderingContent"`
}

// InstreamVideoAdRenderer carries the ad length and optional skip offset.
type InstreamVideoAdRenderer struct {
	PlayerVars             string    `json:"playerVars"`
	SkipOffsetMilliseconds flexFloat `json:"skipOffsetMilliseconds"`
}

// flexFloat accepts a JSON number or numeric string; ok reports presence.
type flexFloat struct {
	v  float64
	ok bool
}

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	f.v, f.ok = v, true
	return nil
}

func adWaitSeconds(r *InstreamVideoAdRenderer) (float64, bool) {
	if r == nil {
		return 0, false
	}
	duration, haveDuration := 0.0, false
	if q, err := url.ParseQuery(r.PlayerVars); err == nil {
		if vals := q["length_seconds"]; len(vals) > 0 {
			if n, err := strconv.Atoi(vals[len(vals)-1]); err == nil {
				duration, haveDuration = float64(n), true
			}
		}
	}
	if r.SkipOffsetMilliseconds.ok {
		// A skippable ad only needs the wait-until-skip time.
		return r.SkipOffsetMilliseconds.v / 1000, true
	}
	return duration, haveDuration
}

// AvailableAtTimestamp ports yt-dlp's _get_available_at_timestamp: start ads
// delay when googlevideo URLs from this response become usable (earlier
// requests are answered 403). Returns 0 when there is nothing to wait for.
func AvailableAtTimestamp(resp *PlayerResponse, now time.Time) int64 {
	if resp == nil {
		return 0
	}
	wait := 0.0
	for _, p := range resp.AdPlacements {
		if p.AdPlacementRenderer.Config.AdPlacementConfig.Kind != "AD_PLACEMENT_KIND_START" {
			continue
		}
		if w, ok := adWaitSeconds(p.AdPlacementRenderer.Renderer.InstreamVideoAdRenderer); ok {
			wait += w
		}
	}
	for _, s := range resp.AdSlots {
		if s.AdSlotRenderer.AdSlotMetadata.TriggerEvent != "SLOT_TRIGGER_EVENT_BEFORE_CONTENT" {
			continue
		}
		layout := s.AdSlotRenderer.FulfillmentContent.FulfilledLayout.PlayerBytesAdLayoutRenderer.RenderingContent
		if w, ok := adWaitSeconds(layout.InstreamVideoAdRenderer); ok {
			wait += w
		}
		for _, seq := range layout.PlayerBytesSequentialLayoutRenderer.SequentialLayouts {
			if w, ok := adWaitSeconds(seq.PlayerBytesAdLayoutRenderer.RenderingContent.InstreamVideoAdRenderer); ok {
				wait += w
			}
		}
	}
	if wait <= 0 {
		return 0
	}
	base := now.Unix()
	if now.Nanosecond() > 0 {
		base++ // math.ceil(now)
	}
	return int64(math.Ceil(float64(base) + wait))
}
