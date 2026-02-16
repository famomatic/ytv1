package client

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func requireE2E(t *testing.T) string {
	t.Helper()
	if os.Getenv("YTV1_E2E") != "1" {
		t.Skip("set YTV1_E2E=1 to run live integration tests")
	}
	videoID := os.Getenv("YTV1_E2E_VIDEO_ID")
	if videoID == "" {
		videoID = "DSYFmhjDbvs"
	}
	return videoID
}

func newE2EClient() *Client {
	return New(Config{
		RequestTimeout: 45 * time.Second,
	})
}

func TestE2E_DSYF_GetVideoAndFormatsSmoke(t *testing.T) {
	videoID := requireE2E(t)
	c := newE2EClient()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	info, err := c.GetVideo(ctx, videoID)
	if err != nil {
		t.Fatalf("GetVideo() error = %v", err)
	}
	if info == nil || len(info.Formats) == 0 {
		t.Fatalf("GetVideo() formats empty: info=%+v", info)
	}
	formats, err := c.GetFormats(ctx, videoID)
	if err != nil {
		t.Fatalf("GetFormats() error = %v", err)
	}
	if len(formats) == 0 {
		t.Fatal("GetFormats() returned no formats")
	}
}

func TestE2E_DSYF_ResolveStreamURLSmoke(t *testing.T) {
	videoID := requireE2E(t)
	c := newE2EClient()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	formats, err := c.GetFormats(ctx, videoID)
	if err != nil {
		t.Fatalf("GetFormats() error = %v", err)
	}
	if len(formats) == 0 {
		t.Fatal("GetFormats() returned no formats")
	}

	var picked FormatInfo
	found := false
	for _, f := range formats {
		if f.HasVideo || f.HasAudio {
			picked = f
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no playable format found for resolve test")
	}

	resolved, err := c.ResolveStreamURL(ctx, videoID, picked.Itag)
	if err != nil {
		t.Fatalf("ResolveStreamURL() error = %v", err)
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		t.Fatalf("resolved url invalid: %q err=%v", resolved, err)
	}
}

func TestE2E_DSYF_DownloadSmoke(t *testing.T) {
	videoID := requireE2E(t)

	out := filepath.Join(t.TempDir(), "e2e-smoke.mp4")
	c := newE2EClient()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := c.Download(ctx, videoID, DownloadOptions{
		OutputPath: out,
		Mode:       SelectionModeBest,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if res == nil {
		t.Fatal("Download() result is nil")
	}
	if res.Bytes <= 0 {
		t.Fatalf("Download() bytes=%d, want >0", res.Bytes)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatalf("output file missing: %v", statErr)
	}
}
