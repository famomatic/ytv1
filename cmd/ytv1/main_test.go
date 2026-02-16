package main

import (
	"strings"
	"testing"
	"time"

	"github.com/famomatic/ytv1/client"
)

func TestFormatExtractionEvent(t *testing.T) {
	got := formatExtractionEvent(client.ExtractionEvent{
		Stage:  "player_api_json",
		Phase:  "start",
		Client: "mweb",
		Detail: "attempt=1",
	})
	want := "[extract] player_api_json:start client=mweb detail=attempt=1"
	if got != want {
		t.Fatalf("formatExtractionEvent()=%q want=%q", got, want)
	}
}

func TestFormatDownloadEvent(t *testing.T) {
	got := formatDownloadEvent(client.DownloadEvent{
		Stage:   "merge",
		Phase:   "complete",
		VideoID: "DSYFmhjDbvs",
		Path:    "out.webm",
		Detail:  "ok",
	})
	want := "[download] merge:complete video_id=DSYFmhjDbvs path=out.webm detail=ok"
	if got != want {
		t.Fatalf("formatDownloadEvent()=%q want=%q", got, want)
	}
}

func TestLifecyclePrinter_ExtractionElapsed(t *testing.T) {
	clock := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, int64(125*time.Millisecond)),
	}
	i := 0
	lp := newLifecyclePrinter(func() time.Time {
		v := clock[i]
		i++
		return v
	})

	_ = lp.formatExtractionEvent(client.ExtractionEvent{
		Stage:  "player_api_json",
		Phase:  "start",
		Client: "web",
	})
	got := lp.formatExtractionEvent(client.ExtractionEvent{
		Stage:  "player_api_json",
		Phase:  "success",
		Client: "web",
		Detail: "ok",
	})
	if !strings.Contains(got, "elapsed_ms=125") {
		t.Fatalf("expected elapsed_ms in output: %q", got)
	}
}

func TestLifecyclePrinter_DownloadElapsedAndSpeed(t *testing.T) {
	clock := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, int64(2*time.Second)),
	}
	i := 0
	lp := newLifecyclePrinter(func() time.Time {
		v := clock[i]
		i++
		return v
	})

	_ = lp.formatDownloadEvent(client.DownloadEvent{
		Stage:   "download",
		Phase:   "start",
		VideoID: "x",
		Path:    "x.f248.video",
		Detail:  "itag=248",
	})
	got := lp.formatDownloadEvent(client.DownloadEvent{
		Stage:   "download",
		Phase:   "complete",
		VideoID: "x",
		Path:    "x.f248.video",
		Detail:  "bytes=10485760",
	})
	if !strings.Contains(got, "elapsed_ms=2000") {
		t.Fatalf("expected elapsed_ms in output: %q", got)
	}
	if !strings.Contains(got, "speed_bps=") || !strings.Contains(got, "speed_mib_s=") {
		t.Fatalf("expected speed fields in output: %q", got)
	}
	if !strings.Contains(got, "part=video") {
		t.Fatalf("expected role field in output: %q", got)
	}
}
