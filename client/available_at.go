package client

import (
	"context"
	"fmt"
	"time"
)

// waitUntilAvailable ports yt-dlp's "Sleeping N seconds as required by the
// site": TV-client responses carry a pre-roll ad and their googlevideo URLs are
// answered 403 until the ad (or its skip offset) has elapsed.
func (c *Client) waitUntilAvailable(ctx context.Context, selected []FormatInfo) error {
	var availableAt int64
	for _, f := range selected {
		if f.AvailableAt > availableAt {
			availableAt = f.AvailableAt
		}
	}
	wait := time.Until(time.Unix(availableAt, 0))
	if availableAt == 0 || wait <= 0 {
		return nil
	}
	c.emitExtractionEvent("download", "sleep", "", fmt.Sprintf("seconds=%d reason=required_by_site", int(wait.Round(time.Second)/time.Second)))
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
