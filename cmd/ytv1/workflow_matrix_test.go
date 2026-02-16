package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
)

func TestWorkflowMatrix_FixtureCoverage(t *testing.T) {
	t.Run("single_item_default", func(t *testing.T) {
		opts := buildDownloadOptions(cli.Options{})
		if opts.Mode != client.SelectionModeBest {
			t.Fatalf("mode=%q want=%q", opts.Mode, client.SelectionModeBest)
		}
		if !opts.Resume {
			t.Fatalf("resume=%v want=true", opts.Resume)
		}
	})

	t.Run("selector_heavy", func(t *testing.T) {
		sel := "bestvideo[ext=mp4]+bestaudio[ext=m4a]/best[ext=mp4]/best"
		opts := buildDownloadOptions(cli.Options{FormatSelector: sel})
		if opts.FormatSelector != sel {
			t.Fatalf("selector=%q want=%q", opts.FormatSelector, sel)
		}
	})

	t.Run("playlist_batch_reporting", func(t *testing.T) {
		items := []client.PlaylistItem{
			{VideoID: "jNQXAC9IVRw", Title: "one"},
			{VideoID: "DSYFmhjDbvs", Title: "two"},
		}
		summary, failures := runPlaylistItems(context.Background(), nil, items, cli.Options{}, func(_ context.Context, _ *client.Client, videoID string, _ cli.Options) error {
			if videoID == "DSYFmhjDbvs" {
				return errors.New("fail")
			}
			return nil
		})
		if summary.Total != 2 || summary.Succeeded != 1 || summary.Failed != 1 || summary.Aborted {
			t.Fatalf("summary=%+v", summary)
		}
		if len(failures) != 1 || failures[0].VideoID != "DSYFmhjDbvs" {
			t.Fatalf("failures=%+v", failures)
		}
	})

	t.Run("subtitle_path", func(t *testing.T) {
		got := subtitleOutputPath("%(title)s.%(ext)s", &client.VideoInfo{
			ID:    "jNQXAC9IVRw",
			Title: "hello/world",
		}, "en", "srt")
		if got != "hello_world.en.srt" {
			t.Fatalf("subtitle path=%q", got)
		}
	})

	t.Run("archive_rerun_idempotency", func(t *testing.T) {
		archivePath := filepath.Join(t.TempDir(), "archive.txt")
		archive, err := newDownloadArchive(archivePath)
		if err != nil {
			t.Fatalf("newDownloadArchive() error = %v", err)
		}
		defer archive.Close()
		if err := archive.Add("jNQXAC9IVRw"); err != nil {
			t.Fatalf("archive.Add() error = %v", err)
		}

		prev := activeDownloadArchive
		activeDownloadArchive = archive
		defer func() { activeDownloadArchive = prev }()

		if !shouldSkipDownloadByArchive("https://www.youtube.com/watch?v=jNQXAC9IVRw") {
			t.Fatalf("expected archive skip hit")
		}
	})
}
