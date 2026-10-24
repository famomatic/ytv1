package client

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestExtractYTInitialPlayerResponseAndApplyMetadata(t *testing.T) {
	page := []byte(`<script>var ytInitialPlayerResponse = {"videoDetails":{"videoId":"x","title":"T","author":"A"},"microformat":{"playerMicroformatRenderer":{"lengthSeconds":"9"}}};var meta = 1;</script>`)
	blob, err := extractYTInitialPlayerResponse(page)
	if err != nil {
		t.Fatal(err)
	}
	var parsed innertube.PlayerResponse
	if err := json.Unmarshal(blob, &parsed); err != nil {
		t.Fatal(err)
	}
	dst := &innertube.PlayerResponse{}
	applyWatchPageMetadata(dst, &parsed)
	if dst.VideoDetails.Title != "T" || dst.Microformat.PlayerMicroformatRenderer.LengthSeconds != "9" {
		t.Fatalf("metadata not applied: %+v", dst)
	}
	keep := &innertube.PlayerResponse{VideoDetails: innertube.VideoDetails{Title: "keep"}}
	applyWatchPageMetadata(keep, &parsed)
	if keep.VideoDetails.Title != "keep" {
		t.Fatalf("existing title overwritten: %q", keep.VideoDetails.Title)
	}
}

func TestWaitUntilAvailable(t *testing.T) {
	c := New(Config{})
	start := time.Now()
	soon := time.Now().Add(1100 * time.Millisecond).Unix()
	if err := c.waitUntilAvailable(context.Background(), []FormatInfo{{AvailableAt: soon}, {}}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatalf("did not wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.waitUntilAvailable(ctx, []FormatInfo{{AvailableAt: time.Now().Add(time.Hour).Unix()}}); err == nil {
		t.Fatalf("expected cancellation error")
	}
	if err := c.waitUntilAvailable(context.Background(), []FormatInfo{{}}); err != nil {
		t.Fatal(err)
	}
}
