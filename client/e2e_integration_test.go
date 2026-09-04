package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/famomatic/puremux/pkg/media"
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

func TestE2E_LiveOpenStreamHasAudioVideo(t *testing.T) {
	if os.Getenv("YTV1_E2E") != "1" {
		t.Skip("set YTV1_E2E=1 to run live integration tests")
	}
	videoID := os.Getenv("YTV1_E2E_LIVE_VIDEO_ID")
	if videoID == "" {
		t.Skip("set YTV1_E2E_LIVE_VIDEO_ID to a currently-live YouTube video")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c := newE2EClient()
	rc, format, err := c.OpenStream(ctx, videoID, StreamOptions{Mode: SelectionModeBest})
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if !format.HasVideo || !format.HasAudio || format.Protocol != "mpegts" {
		t.Fatalf("OpenStream format = %+v, want merged MPEG-TS AV", format)
	}

	buf := make([]byte, 2<<20)
	n, err := io.ReadAtLeast(rc, buf, 1<<20)
	if err != nil {
		t.Fatalf("read merged live stream: %v", err)
	}
	_ = rc.Close()
	assertLiveTSHasAV(t, buf[:n])

	downloadCtx, stopDownload := context.WithCancel(context.Background())
	defer stopDownload()
	stdoutFile, err := os.CreateTemp(t.TempDir(), "live-stdout-*.ts")
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = stdoutFile
	defer func() { os.Stdout = originalStdout }()
	c.config.OnDownloadProgress = func(event DownloadProgressEvent) {
		if event.Downloaded >= 1<<20 {
			stopDownload()
		}
	}
	_, downloadErr := c.Download(downloadCtx, videoID, DownloadOptions{Mode: SelectionModeBest, OutputPath: "-"})
	os.Stdout = originalStdout
	if err := stdoutFile.Close(); err != nil {
		t.Fatal(err)
	}
	if downloadErr != nil && !errors.Is(downloadErr, context.Canceled) {
		t.Fatalf("Download stdout: %v", downloadErr)
	}
	stdoutData, err := os.ReadFile(stdoutFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(stdoutData) < 1<<20 {
		t.Fatalf("Download stdout bytes = %d, want at least %d", len(stdoutData), 1<<20)
	}
	assertLiveTSHasAV(t, stdoutData[:min(len(stdoutData), 2<<20)])
}

func assertLiveTSHasAV(t *testing.T, data []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	demuxer, err := media.Open(ctx, media.MemorySource("live.ts", bytes.Clone(data)), media.OpenOptions{})
	if err != nil {
		t.Fatalf("probe merged live stream: %v", err)
	}
	defer demuxer.Close()
	var video, audio bool
	for _, stream := range demuxer.Streams() {
		video = video || stream.Type == media.MediaVideo
		audio = audio || stream.Type == media.MediaAudio
	}
	if !video || !audio {
		t.Fatalf("merged live streams = %+v, want video+audio", demuxer.Streams())
	}
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

func TestE2E_DSYF_DownloadAudioOnlySmoke(t *testing.T) {
	videoID := requireE2E(t)

	out := filepath.Join(t.TempDir(), "e2e-audio-only.webm")
	c := newE2EClient()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := c.Download(ctx, videoID, DownloadOptions{
		OutputPath: out,
		Mode:       SelectionModeAudioOnly,
	})
	if err != nil {
		t.Fatalf("Download(audioonly) error = %v", err)
	}
	if res == nil {
		t.Fatal("Download(audioonly) result is nil")
	}
	if res.Bytes <= 0 {
		t.Fatalf("Download(audioonly) bytes=%d, want >0", res.Bytes)
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatalf("audio-only output file missing: %v", statErr)
	}
}
