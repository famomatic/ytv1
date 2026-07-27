package main

import (
	"testing"

	"github.com/famomatic/ytv1/client"
)

// TestProgressDurationCompletionUsesFinal verifies that a duration estimate
// reaching 100% before the stream truly ends does NOT mark the download complete
// (which froze the interactive bar and suppressed later events). Only the
// reporter's Final event — or a true byte-total hit — counts as complete.
func TestProgressDurationCompletionUsesFinal(t *testing.T) {
	p := &cliProgressPrinter{completed: map[string]bool{}, interactive: false}
	key := "x|"

	// EXTINF sums routinely exceed the rounded metadata duration, so DownloadedSeconds
	// hits TotalSeconds mid-stream. This must not be treated as complete.
	p.Print(client.DownloadProgressEvent{Path: "x", TotalSeconds: 10, DownloadedSeconds: 10})
	if p.completed[key] {
		t.Fatal("early duration 100% must not mark the download complete")
	}

	// More real progress still arrives afterwards; still not complete.
	p.Print(client.DownloadProgressEvent{Path: "x", TotalSeconds: 10, DownloadedSeconds: 12})
	if p.completed[key] {
		t.Fatal("post-estimate progress must not be suppressed as complete")
	}

	// The reporter's terminal event is the real completion signal.
	p.Print(client.DownloadProgressEvent{Path: "x", TotalSeconds: 10, DownloadedSeconds: 12, Final: true})
	if !p.completed[key] {
		t.Fatal("Final event should mark the download complete")
	}
}
