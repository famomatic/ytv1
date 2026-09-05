package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
)

func processURL(ctx context.Context, c *client.Client, url string, opts cli.Options) error {
	totalStart := time.Now()
	if !opts.NoPlaylist {
		if playlistID, err := client.ExtractPlaylistID(url); err == nil && playlistID != "" {
			return processPlaylist(ctx, c, playlistID, opts)
		}
	}

	if opts.PlayerJSURLOnly {
		return handlePlayerJS(ctx, c, url)
	}
	if shouldSkipDownloadByArchive(url, opts) {
		if opts.BreakOnExisting {
			return errBreakOnExisting
		}
		return nil
	}

	// Bound extraction and metadata work with a wall-clock deadline, but run the
	// media download itself on the parent context. A long VOD's segmented
	// download legitimately runs longer than any fixed budget; wrapping the whole
	// download in this deadline aborted in-flight segment fetches with
	// "context deadline exceeded" once it elapsed (e.g. a multi-hour SOOP VOD
	// failing mid-stream). Per-connection transport timeouts guard a stalled
	// segment instead.
	downloadCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	extractStart := time.Now()
	if err := sleepBeforeExtractionRequest(ctx, opts); err != nil {
		return err
	}
	info, err := c.GetVideo(ctx, url)
	if err != nil {
		if opts.Verbose {
			fmt.Fprintln(statusW(), formatExtractionEvent(client.ExtractionEvent{
				Stage:  "total",
				Phase:  "failure",
				Client: "all",
				Detail: fmt.Sprintf("elapsed_ms=%d", time.Since(extractStart).Milliseconds()),
			}))
		}
		return err
	}
	if activeDownloadArchive != nil && activeDownloadArchive.Has(client.ArchiveID(info)) {
		if opts.BreakOnExisting {
			return errBreakOnExisting
		}
		return nil
	}
	extractMs := time.Since(extractStart).Milliseconds()
	if opts.Verbose {
		fmt.Fprintln(statusW(), formatExtractionEvent(client.ExtractionEvent{
			Stage:  "total",
			Phase:  "complete",
			Client: "all",
			Detail: fmt.Sprintf("elapsed_ms=%d", extractMs),
		}))
	}

	if opts.PrintJSON || opts.DumpSingleJSON {
		payload, err := c.PlaybackJSON(ctx, url, info, buildDownloadOptions(opts))
		if err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(payload); err != nil {
			return err
		}
		return recordForcedArchiveIfRequested(info, opts)
	}

	if opts.ListFormats {
		printFormats(info)
		return recordForcedArchiveIfRequested(info, opts)
	}
	if opts.ListSubs {
		if err := sleepBeforeExtractionRequest(ctx, opts); err != nil {
			return err
		}
		tracks, err := c.GetSubtitleTracks(ctx, url)
		if err != nil {
			return err
		}
		printSubtitleTracks(info, tracks)
		return recordForcedArchiveIfRequested(info, opts)
	}
	if opts.GetThumbnail {
		if err := printThumbnailURL(info); err != nil {
			return err
		}
		return recordForcedArchiveIfRequested(info, opts)
	}
	if hasMetadataGetFlag(opts) {
		resolveURL := func(itag int) (string, error) {
			return c.ResolveStreamURL(ctx, info.ID, itag)
		}
		if err := printRequestedMetadata(info, url, opts, resolveURL); err != nil {
			return err
		}
		return recordForcedArchiveIfRequested(info, opts)
	}

	if opts.WriteInfoJSON {
		resolved, err := c.ResolveVideoURLs(ctx, url, info)
		if err != nil {
			return err
		}
		info = resolved
		if err := writeInfoJSONSidecar(url, info, opts); err != nil {
			return err
		}
	}
	if opts.WriteDescription {
		if err := writeDescriptionSidecar(info, opts); err != nil {
			return err
		}
	}
	if opts.WriteThumbnail {
		if err := writeThumbnailSidecar(ctx, c, info, opts); err != nil {
			return err
		}
	}
	if opts.WriteURLLink {
		if err := writeShortcutSidecar(url, info, opts, shortcutURL); err != nil {
			return err
		}
	}
	if opts.WriteWeblocLink {
		if err := writeShortcutSidecar(url, info, opts, shortcutWebloc); err != nil {
			return err
		}
	}
	if opts.WriteDesktopLink {
		if err := writeShortcutSidecar(url, info, opts, shortcutDesktop); err != nil {
			return err
		}
	}

	if opts.WriteSubs || opts.WriteAutoSubs {
		if err := writeRequestedSubtitles(ctx, c, url, info, opts); err != nil {
			return err
		}
	}

	if opts.SkipDownload {
		if shouldPrintHumanText(opts) {
			fmt.Fprintf(statusW(), "Skipping download for %s\n", info.Title)
		}
		return recordForcedArchiveIfRequested(info, opts)
	}

	if shouldPrintProgressText(opts) {
		fmt.Fprintf(statusW(), "Downloading: %s [%s]\n", info.Title, info.ID)
		activeProgressPrinter.SetTitle(info.Title)
	}
	downloadOpts, err := buildDownloadOptionsForVideo(info, opts)
	if err != nil {
		return err
	}
	if shouldSkipExistingOutput(downloadOpts.OutputPath, opts) {
		if shouldPrintHumanText(opts) {
			fmt.Fprintf(statusW(), "Skipping existing file: %s\n", downloadOpts.OutputPath)
		}
		if err := recordCompletedDownload(client.ArchiveID(info)); err != nil {
			return err
		}
		return nil
	}
	if shouldSkipExistingPostprocessedOutput(info, downloadOpts.OutputPath, opts) {
		if shouldPrintHumanText(opts) {
			fmt.Fprintf(statusW(), "Skipping existing post-processed file: %s\n", downloadOpts.OutputPath)
		}
		if err := recordCompletedDownload(client.ArchiveID(info)); err != nil {
			return err
		}
		return nil
	}
	if err := sleepBeforeMediaDownload(downloadCtx, opts); err != nil {
		return err
	}
	res, err := c.Download(downloadCtx, url, downloadOpts)
	if activeProgressPrinter != nil {
		activeProgressPrinter.Finish()
	}
	if err != nil {
		return err
	}
	if shouldPrintProgressText(opts) {
		fmt.Fprintf(statusW(), "Downloaded to: %s\n", res.OutputPath)
	}
	if err := applyDownloadedFileMTime(res.OutputPath, info, opts); err != nil {
		return err
	}
	if opts.Verbose && verboseLifecyclePrinter != nil {
		timing := verboseLifecyclePrinter.popVideoTiming(info.ID)
		videoMs := timing.DownloadVideoMs
		audioMs := timing.DownloadAudioMs
		if videoMs == 0 && audioMs == 0 && timing.DownloadSingleMs > 0 {
			videoMs = timing.DownloadSingleMs
		}
		downloadTotalMs := videoMs + audioMs
		avgSpeed := "0B/s"
		if downloadTotalMs > 0 {
			bps := int64(float64(res.Bytes) / (float64(downloadTotalMs) / 1000.0))
			avgSpeed = fmt.Sprintf("%dB/s", bps)
		}
		if shouldPrintHumanText(opts) {
			fmt.Fprintf(statusW(),
				"total_elapsed_ms=%d extract_ms=%d download_ms(video/audio)=%d/%d merge_ms=%d final_size=%d avg_speed=%s\n",
				time.Since(totalStart).Milliseconds(),
				extractMs,
				videoMs,
				audioMs,
				timing.MergeMs,
				res.Bytes,
				avgSpeed,
			)
		}
	}
	if err := recordCompletedDownload(client.ArchiveID(info)); err != nil {
		return err
	}
	return nil
}

func shouldSkipExistingOutput(path string, opts cli.Options) bool {
	if !opts.NoOverwrites || strings.TrimSpace(path) == "" || path == "-" {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func shouldSkipExistingPostprocessedOutput(info *client.VideoInfo, path string, opts cli.Options) bool {
	if !opts.NoPostOverwrites || strings.TrimSpace(path) == "" {
		return false
	}
	if !isPostprocessedOutput(info, opts) {
		return false
	}
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func isPostprocessedOutput(info *client.VideoInfo, opts cli.Options) bool {
	downloadOpts := buildDownloadOptions(opts)
	if downloadOpts.Mode == client.SelectionModeMP3 {
		return true
	}
	if info == nil {
		return false
	}
	formats, err := selectedFormatsForOptions(info.Formats, opts)
	if err != nil {
		return false
	}
	return isMergedSelection(formats)
}

func applyDownloadedFileMTime(path string, info *client.VideoInfo, opts cli.Options) error {
	if !opts.UpdateMTime || strings.TrimSpace(path) == "" || path == "-" {
		return nil
	}
	mt, ok := client.MediaFileMTime(info)
	if !ok {
		return nil
	}
	if err := os.Chtimes(path, mt, mt); err != nil {
		return fmt.Errorf("failed to update media mtime: %w", err)
	}
	return nil
}
