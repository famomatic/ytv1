package main

import (
	"testing"

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

