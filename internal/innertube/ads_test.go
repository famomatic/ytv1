package innertube

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAvailableAtTimestamp(t *testing.T) {
	now := time.Unix(1000, 0)
	cases := []struct {
		name string
		raw  string
		want int64
	}{
		{"no ads", `{}`, 0},
		{"placement skippable uses skip offset", `{"adPlacements":[{"adPlacementRenderer":{"config":{"adPlacementConfig":{"kind":"AD_PLACEMENT_KIND_START"}},"renderer":{"instreamVideoAdRenderer":{"playerVars":"a=b&length_seconds=6","skipOffsetMilliseconds":5000}}}}]}`, 1005},
		{"placement unskippable uses length", `{"adPlacements":[{"adPlacementRenderer":{"config":{"adPlacementConfig":{"kind":"AD_PLACEMENT_KIND_START"}},"renderer":{"instreamVideoAdRenderer":{"playerVars":"length_seconds=15"}}}}]}`, 1015},
		{"non-start placement ignored", `{"adPlacements":[{"adPlacementRenderer":{"config":{"adPlacementConfig":{"kind":"AD_PLACEMENT_KIND_END"}},"renderer":{"instreamVideoAdRenderer":{"playerVars":"length_seconds=15"}}}}]}`, 0},
		{"slot sequential layouts sum", `{"adSlots":[{"adSlotRenderer":{"adSlotMetadata":{"triggerEvent":"SLOT_TRIGGER_EVENT_BEFORE_CONTENT"},"fulfillmentContent":{"fulfilledLayout":{"playerBytesAdLayoutRenderer":{"renderingContent":{"playerBytesSequentialLayoutRenderer":{"sequentialLayouts":[{"playerBytesAdLayoutRenderer":{"renderingContent":{"instreamVideoAdRenderer":{"playerVars":"length_seconds=5"}}}},{"playerBytesAdLayoutRenderer":{"renderingContent":{"instreamVideoAdRenderer":{"playerVars":"length_seconds=7","skipOffsetMilliseconds":"2000"}}}}]}}}}}}}]}`, 1007},
	}
	for _, tc := range cases {
		var resp PlayerResponse
		if err := json.Unmarshal([]byte(tc.raw), &resp); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := AvailableAtTimestamp(&resp, now); got != tc.want {
			t.Fatalf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}
