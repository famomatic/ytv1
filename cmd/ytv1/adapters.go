package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
	"github.com/famomatic/ytv1/internal/playerjs"
)

func sleepBeforeExtractionRequest(ctx context.Context, opts cli.Options) error {
	if opts.SleepRequests <= 0 {
		return nil
	}
	return sleepWithContext(ctx, opts.SleepRequests, sleepBeforeRequestFunc)
}

func sleepBeforeMediaDownload(ctx context.Context, opts cli.Options) error {
	d := mediaDownloadSleepDuration(opts)
	if d <= 0 {
		return nil
	}
	return sleepWithContext(ctx, d, sleepBeforeDownloadFunc)
}

func mediaDownloadSleepDuration(opts cli.Options) time.Duration {
	minSleep := opts.SleepInterval
	if minSleep <= 0 {
		return 0
	}
	if opts.MaxSleepInterval > minSleep {
		// yt-dlp semantics: sleep a uniformly random duration in [min, max]
		// so downloads are not perfectly periodic.
		return minSleep + time.Duration(rand.Int63n(int64(opts.MaxSleepInterval-minSleep)+1))
	}
	return minSleep
}

func sleepBeforeSubtitleDownload(ctx context.Context, opts cli.Options) error {
	if opts.SleepSubtitles <= 0 {
		return nil
	}
	return sleepWithContext(ctx, opts.SleepSubtitles, sleepBeforeSubtitleFunc)
}

func sleepWithContext(ctx context.Context, d time.Duration, sleepFn func(time.Duration)) error {
	if d <= 0 {
		return nil
	}
	done := make(chan struct{})
	go func() {
		sleepFn(d)
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func buildDownloadOptions(opts cli.Options) client.DownloadOptions {
	return client.BuildDownloadOptions(client.DownloadOptionsRequest{
		FormatSelector:        opts.FormatSelector,
		OutputTemplate:        opts.OutputTemplate,
		OutputUseID:           opts.OutputUseID,
		OutputPathDir:         opts.OutputPathDir,
		ExtractAudio:          opts.ExtractAudio,
		AudioFormat:           opts.AudioFormat,
		KeepIntermediateFiles: opts.KeepVideo,
		Resume:                !opts.NoContinue,
		UsePartFiles:          !opts.NoPart,
		AudioQuality:          opts.AudioQuality,
		EmbedMetadata:         opts.EmbedMetadata,
		MergeOutputFormat:     opts.MergeOutputFormat,
		RemuxVideo:            opts.RemuxVideo,
	})
}

func buildDownloadOptionsForVideo(info *client.VideoInfo, opts cli.Options) (client.DownloadOptions, error) {
	downloadOpts := buildDownloadOptions(opts)
	if info == nil {
		return downloadOpts, nil
	}
	outputPath, err := predictedOutputFilename(info, opts)
	if err != nil {
		return client.DownloadOptions{}, err
	}
	downloadOpts.OutputPath = outputPath
	return downloadOpts, nil
}

func effectiveOutputTemplate(opts cli.Options) string {
	return client.EffectiveOutputTemplate(client.OutputTemplateOptions{
		OutputTemplate: opts.OutputTemplate,
		OutputUseID:    opts.OutputUseID,
		OutputPathDir:  opts.OutputPathDir,
	})
}

func processPlaylist(ctx context.Context, c *client.Client, playlistID string, opts cli.Options) error {
	if shouldPrintPlaylistText(opts) {
		fmt.Printf("Fetching playlist: %s\n", playlistID)
	}
	if err := sleepBeforeExtractionRequest(ctx, opts); err != nil {
		return err
	}
	playlist, err := c.GetPlaylist(ctx, playlistID)
	if err != nil {
		return err
	}
	if shouldPrintPlaylistText(opts) {
		fmt.Printf("Playlist: %s (%d videos)\n", playlist.Title, len(playlist.Items))
	}
	if opts.WriteInfoJSON && !opts.NoWritePlaylistMetafiles {
		if err := writePlaylistInfoJSONSidecar(playlist, opts); err != nil {
			return err
		}
	}
	itemsWithIndex := withPlaylistIndexes(playlist.Items)
	items, err := client.SelectPlaylistItems(itemsWithIndex, playlistSelector(opts))
	if err != nil {
		return err
	}
	items = orderPlaylistItems(items, opts)
	if opts.FlatPlaylist {
		return emitFlatPlaylist(items, opts, os.Stdout)
	}

	playlistCtx := playlistTemplateContext{
		ID:         playlist.ID,
		Title:      playlist.Title,
		Uploader:   playlist.Uploader,
		UploaderID: playlist.UploaderID,
		Channel:    playlist.Channel,
		ChannelID:  playlist.ChannelID,
		Count:      len(items),
	}
	summary, failures := runPlaylistItems(ctx, c, items, playlistCtx, opts, processURL)
	if shouldPrintPlaylistText(opts) {
		fmt.Printf(
			"Playlist summary: total=%d succeeded=%d failed=%d skipped=%d aborted=%t\n",
			summary.Total,
			summary.Succeeded,
			summary.Failed,
			summary.Skipped,
			summary.Aborted,
		)
	}
	if len(failures) > 0 && shouldPrintPlaylistText(opts) {
		for _, failure := range failures {
			log.Printf("Failed to process %s: %v", failure.VideoID, failure.Err)
		}
	}
	// Propagate an intentional abort (max-downloads reached, break-on-existing,
	// cancellation) so the top-level input loop stops instead of continuing to
	// the next URL and downloading past the global cap.
	if summary.AbortErr != nil {
		return summary.AbortErr
	}
	if len(failures) > 0 {
		return fmt.Errorf("playlist completed with failures: failed=%d/%d", summary.Failed, summary.Total)
	}
	return nil
}

func playlistSelector(opts cli.Options) string {
	if strings.TrimSpace(opts.PlaylistItems) != "" {
		return opts.PlaylistItems
	}
	if opts.PlaylistStart <= 0 && opts.PlaylistEnd <= 0 {
		return ""
	}
	start := ""
	end := ""
	if opts.PlaylistStart > 0 {
		start = strconv.Itoa(opts.PlaylistStart)
	}
	if opts.PlaylistEnd > 0 {
		end = strconv.Itoa(opts.PlaylistEnd)
	}
	return start + ":" + end
}

func orderPlaylistItems(items []client.PlaylistItem, opts cli.Options) []client.PlaylistItem {
	if opts.PlaylistReverse {
		return client.OrderPlaylistItems(items, client.PlaylistOrderReverse)
	}
	if opts.PlaylistRandom {
		return client.OrderPlaylistItems(items, client.PlaylistOrderRandom)
	}
	return items
}

func reversePlaylistItems(items []client.PlaylistItem) []client.PlaylistItem {
	return client.ReversePlaylistItems(items)
}

func shufflePlaylistItems(items []client.PlaylistItem, rng *rand.Rand) []client.PlaylistItem {
	return client.ShufflePlaylistItems(items, rng)
}

func shouldPrintPlaylistText(opts cli.Options) bool {
	return shouldPrintHumanText(opts)
}

func shouldPrintHumanText(opts cli.Options) bool {
	return !opts.PrintJSON && !opts.DumpSingleJSON && !opts.Quiet
}

func shouldPrintProgressText(opts cli.Options) bool {
	if opts.PrintJSON || opts.DumpSingleJSON {
		return false
	}
	if opts.NoProgress {
		return false
	}
	return opts.Progress || !opts.Quiet
}

func emitFlatPlaylist(items []client.PlaylistItem, opts cli.Options, w io.Writer) error {
	return client.WriteFlatPlaylist(w, items, opts.PrintJSON)
}

type playlistRunSummary = client.PlaylistRunSummary
type playlistItemFailure = client.PlaylistItemFailure
type playlistTemplateContext = client.PlaylistTemplateContext

func runPlaylistItems(
	ctx context.Context,
	c *client.Client,
	items []client.PlaylistItem,
	playlistCtx playlistTemplateContext,
	opts cli.Options,
	processor func(context.Context, *client.Client, string, cli.Options) error,
) (playlistRunSummary, []playlistItemFailure) {
	return client.RunPlaylistItems(ctx, items, client.PlaylistRunOptions{
		AbortOnError:    opts.AbortOnError,
		SkipAfterErrors: opts.SkipPlaylistAfterErrors,
		IsBreakOnExisting: func(err error) bool {
			return errors.Is(err, errBreakOnExisting)
		},
		IsMaxDownloadsReached: func(err error) bool {
			return errors.Is(err, errMaxDownloadsReached)
		},
		OnItemStart: func(index int, total int, item client.PlaylistItem) {
			if shouldPrintProgressText(opts) {
				fmt.Printf("[%d/%d] Processing %s (%s)...\n", index, total, item.Title, item.VideoID)
			}
		},
	}, func(ctx context.Context, item client.PlaylistItem, autonumber int) error {
		itemOpts := applyPlaylistTemplateContext(opts, item, autonumber, playlistCtx)
		return processor(ctx, c, item.VideoID, itemOpts)
	})
}

func withPlaylistIndexes(items []client.PlaylistItem) []client.PlaylistItem {
	out := append([]client.PlaylistItem(nil), items...)
	for i := range out {
		if out[i].PlaylistIndex <= 0 {
			out[i].PlaylistIndex = i + 1
		}
	}
	return out
}

func applyPlaylistTemplateContext(opts cli.Options, item client.PlaylistItem, autonumber int, playlistCtx playlistTemplateContext) cli.Options {
	opts.OutputTemplate = client.ApplyPlaylistTemplateContext(opts.OutputTemplate, item, autonumber, client.PlaylistTemplateContext(playlistCtx))
	return opts
}

func reachedPlaylistFailureThreshold(failures int, threshold int) bool {
	return client.ReachedPlaylistFailureThreshold(failures, threshold)
}

func printFormats(info *client.VideoInfo) {
	_ = client.WriteFormatList(os.Stdout, info)
}

func printSubtitleTracks(info *client.VideoInfo, tracks []client.SubtitleTrack) {
	_ = client.WriteSubtitleTrackList(os.Stdout, info, tracks)
}

func printThumbnailURL(info *client.VideoInfo) error {
	if info == nil || strings.TrimSpace(info.ThumbnailURL) == "" {
		videoID := ""
		if info != nil {
			videoID = info.ID
		}
		return fmt.Errorf("%w: thumbnail unavailable for video=%s", client.ErrUnavailable, videoID)
	}
	fmt.Println(info.ThumbnailURL)
	return nil
}

func hasMetadataGetFlag(opts cli.Options) bool {
	return opts.GetTitle || opts.GetID || opts.GetDescription || opts.GetDuration || opts.GetFormat || opts.GetFilename || opts.GetURL || len(opts.PrintTemplates) > 0 || len(opts.PrintToFile) > 0 || len(opts.GetMetadataOrder) > 0
}

type streamURLResolver func(itag int) (string, error)

func printRequestedMetadata(info *client.VideoInfo, input string, opts cli.Options, resolveURL streamURLResolver) error {
	items, err := client.RenderMetadataPrintItems(info, input, metadataPrintRequest(opts), client.StreamURLResolver(resolveURL))
	if err != nil {
		return err
	}
	for _, item := range items {
		if strings.TrimSpace(item.File) != "" {
			if err := client.AppendMetadataPrintFile(item.File, item.Value); err != nil {
				return err
			}
			continue
		}
		fmt.Println(item.Value)
	}
	return nil
}

func parsePrintFileOrderField(raw string) (cli.PrintToFileSpec, bool) {
	spec, ok := client.ParseMetadataPrintFileOrderField(raw)
	if !ok {
		return cli.PrintToFileSpec{}, false
	}
	return cli.PrintToFileSpec{Template: spec.Template, File: spec.File}, true
}

func appendPrintToFile(spec cli.PrintToFileSpec, info *client.VideoInfo, input string, opts cli.Options, resolveURL streamURLResolver) error {
	items, err := client.RenderMetadataPrintItems(info, input, client.MetadataPrintRequest{
		Order:             []string{"printfile:" + spec.Template + "\x00" + spec.File},
		DownloadOpts:      buildDownloadOptions(opts),
		FilenameOpts:      metadataFilenameOptions(opts),
		RestrictFilenames: opts.RestrictFilenames,
	}, client.StreamURLResolver(resolveURL))
	if err != nil {
		return err
	}
	for _, item := range items {
		if strings.TrimSpace(item.File) != "" {
			if err := client.AppendMetadataPrintFile(item.File, item.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderPrintTemplate(template string, info *client.VideoInfo, input string, opts cli.Options, resolveURL streamURLResolver) (string, error) {
	return client.RenderMetadataPrintField(template, info, input, metadataPrintRequest(opts), client.StreamURLResolver(resolveURL))
}

func metadataPrintRequest(opts cli.Options) client.MetadataPrintRequest {
	order := opts.GetMetadataOrder
	if len(order) == 0 {
		if opts.GetTitle {
			order = append(order, "title")
		}
		if opts.GetID {
			order = append(order, "id")
		}
		if opts.GetDescription {
			order = append(order, "description")
		}
		if opts.GetDuration {
			order = append(order, "duration")
		}
		if opts.GetFilename {
			order = append(order, "filename")
		}
		if opts.GetFormat {
			order = append(order, "format")
		}
		if opts.GetURL {
			order = append(order, "url")
		}
	}
	printFiles := make([]client.MetadataPrintFileSpec, 0, len(opts.PrintToFile))
	for _, spec := range opts.PrintToFile {
		printFiles = append(printFiles, client.MetadataPrintFileSpec{Template: spec.Template, File: spec.File})
	}
	return client.MetadataPrintRequest{
		Order:             order,
		PrintFields:       opts.PrintTemplates,
		PrintToFile:       printFiles,
		DownloadOpts:      buildDownloadOptions(opts),
		FilenameOpts:      metadataFilenameOptions(opts),
		RestrictFilenames: opts.RestrictFilenames,
	}
}

func metadataFilenameOptions(opts cli.Options) client.OutputFilenameOptions {
	downloadOpts := buildDownloadOptions(opts)
	return client.OutputFilenameOptions{
		OutputTemplate:    downloadOpts.OutputPath,
		Mode:              downloadOpts.Mode,
		MergeOutputExt:    effectiveCLIMergeOutputExt(opts),
		TrimFilenames:     opts.TrimFilenames,
		RestrictFilenames: opts.RestrictFilenames,
	}
}

func effectiveCLIMergeOutputExt(opts cli.Options) string {
	return client.EffectiveMergeOutputExt(opts.MergeOutputFormat, opts.RemuxVideo)
}

func predictedOutputFilename(info *client.VideoInfo, opts cli.Options) (string, error) {
	formats, err := selectedFormatsForOptions(info.Formats, opts)
	if err != nil {
		return "", err
	}
	downloadOpts := buildDownloadOptions(opts)
	return client.PredictOutputFilename(info, formats, client.OutputFilenameOptions{
		OutputTemplate:    downloadOpts.OutputPath,
		Mode:              downloadOpts.Mode,
		MergeOutputExt:    effectiveCLIMergeOutputExt(opts),
		TrimFilenames:     opts.TrimFilenames,
		RestrictFilenames: opts.RestrictFilenames,
	})
}

func selectedStreamURLs(info *client.VideoInfo, opts cli.Options, resolveURL streamURLResolver) ([]string, error) {
	formats, err := selectedFormatsForOptions(info.Formats, opts)
	if err != nil {
		return nil, err
	}
	return client.SelectedStreamURLs(formats, client.StreamURLResolver(resolveURL))
}

func isMergedSelection(formats []client.FormatInfo) bool {
	return client.IsMergedSelection(formats)
}

func selectedFormatsForOptions(formats []client.FormatInfo, opts cli.Options) ([]client.FormatInfo, error) {
	return client.SelectFormatsForDownloadOptions(formats, buildDownloadOptions(opts))
}

func formatSelectedFormats(formats []client.FormatInfo) string {
	return client.FormatSummaries(formats)
}

func handlePlayerJS(ctx context.Context, c *client.Client, videoID string) error {
	resolver := playerjs.NewResolver(nil, playerjs.NewMemoryCache())
	path, err := resolver.GetPlayerURL(ctx, videoID)
	if err != nil {
		return err
	}
	if strings.HasPrefix(path, "http") {
		fmt.Println(path)
	} else {
		fmt.Println("https://www.youtube.com" + path)
	}
	return nil
}

func writeRequestedSubtitles(
	ctx context.Context,
	c *client.Client,
	input string,
	info *client.VideoInfo,
	opts cli.Options,
) error {
	subFormat := client.ResolveSubtitleOutputFormat(opts.SubFormat)
	if opts.AllSubs || subtitleLanguagesRequestAll(parseSubtitleLanguages(opts.SubLangs)) {
		if err := sleepBeforeExtractionRequest(ctx, opts); err != nil {
			return err
		}
	}
	result, err := c.WriteRequestedSubtitleSidecars(ctx, input, info, client.SubtitleSidecarOptions{
		Languages:         parseSubtitleLanguages(opts.SubLangs),
		RequestAll:        opts.AllSubs,
		IncludeManual:     opts.WriteSubs || !opts.WriteAutoSubs,
		IncludeAuto:       opts.WriteAutoSubs,
		OutputFormat:      subFormat,
		OutputTemplate:    effectiveOutputTemplate(opts),
		RestrictFilenames: opts.RestrictFilenames,
		TrimFilenames:     opts.TrimFilenames,
		NoOverwrites:      opts.NoOverwrites,
		BeforeEach: func(ctx context.Context) error {
			return sleepBeforeSubtitleDownload(ctx, opts)
		},
	})
	for _, outcome := range result.Outcomes {
		if outcome.Skipped && shouldPrintHumanText(opts) {
			fmt.Printf("Skipping existing subtitle: %s\n", outcome.Path)
		}
		if outcome.Written && shouldPrintHumanText(opts) {
			fmt.Printf("Written subtitle: %s\n", outcome.Path)
		}
	}
	if err != nil {
		return err
	}
	failures := result.FailureMessages()
	if len(failures) > 0 {
		warnf(opts, "subtitle partial failure: %s", strings.Join(failures, "; "))
	}
	return nil
}

func warnf(opts cli.Options, format string, args ...any) {
	if opts.NoWarnings || opts.Quiet {
		return
	}
	log.Printf("WARNING: "+format, args...)
}

func parseSubtitleLanguages(raw string) []string {
	return client.ParseSubtitleLanguages(raw)
}

func subtitleLanguagesRequestAll(langs []string) bool {
	return client.SubtitleLanguagesRequestAll(langs)
}

func subtitleLanguagesFromTracksForRequest(tracks []client.SubtitleTrack, opts cli.Options, requested []string) []string {
	return client.SubtitleLanguagesFromTracksForRequest(tracks, requested, subtitleLanguageSelection(opts))
}

func subtitleLanguagesFromTracks(tracks []client.SubtitleTrack, opts cli.Options) []string {
	return client.SubtitleLanguagesFromTracks(tracks, subtitleLanguageSelection(opts))
}

func subtitleLanguageSelection(opts cli.Options) client.SubtitleLanguageSelection {
	return client.SubtitleLanguageSelection{
		IncludeManual: opts.WriteSubs || !opts.WriteAutoSubs,
		IncludeAuto:   opts.WriteAutoSubs,
	}
}

func subtitleOutputPath(outputTemplate string, info *client.VideoInfo, lang string, outputExt string, restrictFilenamesAndTrim ...any) string {
	restrict, trimLimit := templatePathOptions(restrictFilenamesAndTrim...)
	return client.SubtitleOutputPath(info, lang, outputExt, client.SidecarPathOptions{
		OutputTemplate:    outputTemplate,
		RestrictFilenames: restrict,
		TrimFilenames:     trimLimit,
	})
}

func templatePathOptions(values ...any) (bool, int) {
	restrict := false
	trimLimit := 0
	if len(values) > 0 {
		if v, ok := values[0].(bool); ok {
			restrict = v
		}
	}
	if len(values) > 1 {
		if v, ok := values[1].(int); ok {
			trimLimit = v
		}
	}
	return restrict, trimLimit
}

func writeInfoJSONSidecar(input string, info *client.VideoInfo, opts cli.Options) error {
	outputPath := infoJSONOutputPath(effectiveOutputTemplate(opts), info, opts.RestrictFilenames, opts.TrimFilenames)
	if shouldSkipExistingOutput(outputPath, opts) {
		if shouldPrintHumanText(opts) {
			fmt.Printf("Skipping existing info JSON: %s\n", outputPath)
		}
		return nil
	}
	if err := client.WriteInfoJSONSidecar(input, info, outputPath); err != nil {
		return err
	}
	if shouldPrintHumanText(opts) {
		fmt.Printf("Written info JSON: %s\n", outputPath)
	}
	return nil
}

func writePlaylistInfoJSONSidecar(playlist *client.PlaylistInfo, opts cli.Options) error {
	info := playlistInfoAsVideoInfo(playlist)
	outputPath := infoJSONOutputPath(effectiveOutputTemplate(opts), info, opts.RestrictFilenames, opts.TrimFilenames)
	if shouldSkipExistingOutput(outputPath, opts) {
		if shouldPrintHumanText(opts) {
			fmt.Printf("Skipping existing playlist info JSON: %s\n", outputPath)
		}
		return nil
	}
	if err := client.WritePlaylistInfoJSONSidecar(playlist, outputPath); err != nil {
		return err
	}
	if shouldPrintHumanText(opts) {
		fmt.Printf("Written playlist info JSON: %s\n", outputPath)
	}
	return nil
}

func playlistInfoAsVideoInfo(playlist *client.PlaylistInfo) *client.VideoInfo {
	return client.PlaylistInfoAsVideoInfo(playlist)
}

func infoJSONOutputPath(outputTemplate string, info *client.VideoInfo, restrictFilenamesAndTrim ...any) string {
	restrict, trimLimit := templatePathOptions(restrictFilenamesAndTrim...)
	return client.InfoJSONOutputPath(info, client.SidecarPathOptions{
		OutputTemplate:    outputTemplate,
		RestrictFilenames: restrict,
		TrimFilenames:     trimLimit,
	})
}

func writeDescriptionSidecar(info *client.VideoInfo, opts cli.Options) error {
	outputPath := descriptionOutputPath(effectiveOutputTemplate(opts), info, opts.RestrictFilenames, opts.TrimFilenames)
	if shouldSkipExistingOutput(outputPath, opts) {
		if shouldPrintHumanText(opts) {
			fmt.Printf("Skipping existing description: %s\n", outputPath)
		}
		return nil
	}
	if err := client.WriteDescriptionSidecar(info, outputPath); err != nil {
		return err
	}
	if shouldPrintHumanText(opts) {
		fmt.Printf("Written description: %s\n", outputPath)
	}
	return nil
}

type shortcutKind string

const (
	shortcutURL     shortcutKind = "url"
	shortcutWebloc  shortcutKind = "webloc"
	shortcutDesktop shortcutKind = "desktop"
)

func writeShortcutSidecar(input string, info *client.VideoInfo, opts cli.Options, kind shortcutKind) error {
	outputPath := shortcutOutputPath(effectiveOutputTemplate(opts), info, kind, opts.RestrictFilenames, opts.TrimFilenames)
	if shouldSkipExistingOutput(outputPath, opts) {
		if shouldPrintHumanText(opts) {
			fmt.Printf("Skipping existing %s link: %s\n", kind, outputPath)
		}
		return nil
	}
	if err := client.WriteShortcutSidecar(input, info, client.ShortcutKind(kind), outputPath); err != nil {
		return err
	}
	if shouldPrintHumanText(opts) {
		fmt.Printf("Written %s link: %s\n", kind, outputPath)
	}
	return nil
}

func urlLinkOutputPath(outputTemplate string, info *client.VideoInfo, restrictFilenamesAndTrim ...any) string {
	return shortcutOutputPath(outputTemplate, info, shortcutURL, restrictFilenamesAndTrim...)
}

func shortcutOutputPath(outputTemplate string, info *client.VideoInfo, kind shortcutKind, restrictFilenamesAndTrim ...any) string {
	restrict, trimLimit := templatePathOptions(restrictFilenamesAndTrim...)
	return client.ShortcutOutputPath(info, client.ShortcutKind(kind), client.SidecarPathOptions{
		OutputTemplate:    outputTemplate,
		RestrictFilenames: restrict,
		TrimFilenames:     trimLimit,
	})
}

func descriptionOutputPath(outputTemplate string, info *client.VideoInfo, restrictFilenamesAndTrim ...any) string {
	restrict, trimLimit := templatePathOptions(restrictFilenamesAndTrim...)
	return client.DescriptionOutputPath(info, client.SidecarPathOptions{
		OutputTemplate:    outputTemplate,
		RestrictFilenames: restrict,
		TrimFilenames:     trimLimit,
	})
}

func writeThumbnailSidecar(ctx context.Context, c *client.Client, info *client.VideoInfo, opts cli.Options) error {
	outputPath := thumbnailOutputPath(effectiveOutputTemplate(opts), info, opts.RestrictFilenames, opts.TrimFilenames)
	if shouldSkipExistingOutput(outputPath, opts) {
		if shouldPrintHumanText(opts) {
			fmt.Printf("Skipping existing thumbnail: %s\n", outputPath)
		}
		return nil
	}
	if err := c.DownloadThumbnail(ctx, info, outputPath); err != nil {
		return err
	}
	if shouldPrintHumanText(opts) {
		fmt.Printf("Written thumbnail: %s\n", outputPath)
	}
	return nil
}

func thumbnailOutputPath(outputTemplate string, info *client.VideoInfo, restrictFilenamesAndTrim ...any) string {
	restrict, trimLimit := templatePathOptions(restrictFilenamesAndTrim...)
	return client.ThumbnailOutputPath(info, client.SidecarPathOptions{
		OutputTemplate:    outputTemplate,
		RestrictFilenames: restrict,
		TrimFilenames:     trimLimit,
	})
}

func shouldSkipDownloadByArchive(input string, opts cli.Options) bool {
	if activeDownloadArchive == nil {
		return false
	}
	videoID, err := client.ExtractVideoID(input)
	if err != nil {
		return false
	}
	if !activeDownloadArchive.Has(videoID) {
		return false
	}
	if shouldPrintHumanText(opts) {
		fmt.Printf("Skipping (in archive): %s\n", videoID)
	}
	return true
}

func recordCompletedDownload(videoID string) error {
	if activeDownloadArchive == nil {
		return incrementDownloadLimit()
	}
	if err := activeDownloadArchive.Add(videoID); err != nil {
		return fmt.Errorf("failed to update download archive: %w", err)
	}
	return incrementDownloadLimit()
}

func recordForcedArchiveIfRequested(info *client.VideoInfo, opts cli.Options) error {
	if !opts.ForceWriteArchive || info == nil {
		return nil
	}
	return recordCompletedDownload(client.ArchiveID(info))
}

type downloadLimit struct {
	Max   int
	Count atomic.Int64
}

func incrementDownloadLimit() error {
	if activeDownloadLimit == nil || activeDownloadLimit.Max <= 0 {
		return nil
	}
	// Atomic read-modify-write so concurrent playlist/download paths
	// cannot lose increments once batch processing is parallelized.
	next := activeDownloadLimit.Count.Add(1)
	if next >= int64(activeDownloadLimit.Max) {
		return errMaxDownloadsReached
	}
	return nil
}

func printAttemptDiagnostics(err error) {
	attempts, ok := client.AttemptDetails(err)
	if !ok || len(attempts) == 0 {
		printGenericRemediationHints(err)
		return
	}
	fmt.Println("Attempt diagnostics:")
	for i, a := range attempts {
		fmt.Printf("  [%d] client=%s stage=%s", i+1, a.Client, a.Stage)
		if a.Itag != 0 {
			fmt.Printf(" itag=%d", a.Itag)
		}
		if a.Protocol != "" {
			fmt.Printf(" proto=%s", a.Protocol)
		}
		if a.HTTPStatus != 0 {
			fmt.Printf(" http=%d", a.HTTPStatus)
		}
		if a.URLHost != "" {
			fmt.Printf(" host=%s", a.URLHost)
		}
		if a.URLHasN {
			fmt.Printf(" has_n=true")
		}
		if a.URLHasPOT {
			fmt.Printf(" has_pot=true")
		}
		if a.URLHasSignature {
			fmt.Printf(" has_sig=true")
		}
		if a.POTRequired {
			fmt.Printf(" pot_required=true")
		}
		if a.Reason != "" {
			fmt.Printf(" reason=%q", a.Reason)
		}
		fmt.Println()
	}
	for _, hint := range remediationHintsForAttempts(attempts) {
		fmt.Println(hint)
	}
}

func printGenericRemediationHints(err error) {
	for _, hint := range client.GenericRemediationHints(err) {
		fmt.Println(hint)
	}
}

func remediationHintsForAttempts(attempts []client.AttemptDetail) []string {
	return client.RemediationHintsForAttempts(attempts)
}

func formatExtractionEvent(evt client.ExtractionEvent) string {
	return client.FormatExtractionEvent(evt)
}

func formatDownloadEvent(evt client.DownloadEvent) string {
	return client.FormatDownloadEvent(evt)
}

type lifecyclePrinter struct {
	inner *client.LifecyclePrinter
}

func newLifecyclePrinter(now func() time.Time) *lifecyclePrinter {
	return &lifecyclePrinter{inner: client.NewLifecyclePrinter(now)}
}

type videoTiming = client.VideoTiming

func (p *lifecyclePrinter) formatExtractionEvent(evt client.ExtractionEvent) string {
	return p.inner.FormatExtractionEvent(evt)
}

func (p *lifecyclePrinter) formatDownloadEvent(evt client.DownloadEvent) string {
	return p.inner.FormatDownloadEvent(evt)
}

func (p *lifecyclePrinter) popVideoTiming(videoID string) videoTiming {
	return p.inner.PopVideoTiming(videoID)
}

type ytdlpDumpSingleJSON = client.YTDLPDumpSingleJSON
type ytdlpPlaylistInfoJSON = client.YTDLPPlaylistInfoJSON
type ytdlpPlaylistItemEntry = client.YTDLPPlaylistItemEntry
type ytdlpFormatEntry = client.YTDLPFormatEntry

func emitDumpSingleJSON(w io.Writer, input string, info *client.VideoInfo) error {
	payload := buildDumpSingleJSONPayload(input, info)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func buildPlaylistInfoJSONPayload(playlist *client.PlaylistInfo) ytdlpPlaylistInfoJSON {
	return client.BuildYTDLPPlaylistInfoJSON(playlist)
}

func buildDumpSingleJSONPayload(input string, info *client.VideoInfo) ytdlpDumpSingleJSON {
	return client.BuildYTDLPDumpSingleJSON(input, info)
}

type cliErrorReport struct {
	OK       bool           `json:"ok"`
	Input    string         `json:"input"`
	ExitCode int            `json:"exit_code"`
	Error    cliErrorDetail `json:"error"`
}

type cliErrorDetail struct {
	Category string                 `json:"category"`
	Message  string                 `json:"message"`
	Attempts []client.AttemptDetail `json:"attempts,omitempty"`
}

func emitJSONFailure(input string, err error, exitCode int) {
	report := cliErrorReport{
		OK:       false,
		Input:    input,
		ExitCode: exitCode,
		Error: cliErrorDetail{
			Category: string(client.ClassifyError(err)),
			Message:  err.Error(),
		},
	}
	if attempts, ok := client.AttemptDetails(err); ok && len(attempts) > 0 {
		report.Error.Attempts = attempts
	}
	_ = json.NewEncoder(os.Stdout).Encode(report)
}

func classifyExitCode(err error) int {
	switch client.ClassifyError(err) {
	case client.ErrorCategoryInvalidInput:
		return exitCodeInvalidInput
	case client.ErrorCategoryLoginRequired:
		return exitCodeLoginRequired
	case client.ErrorCategoryUnavailable:
		return exitCodeUnavailable
	case client.ErrorCategoryNoPlayableFormats:
		return exitCodeNoPlayableFormats
	case client.ErrorCategoryChallengeNotSolved:
		return exitCodeChallengeUnresolved
	case client.ErrorCategoryAllClientsFailed:
		return exitCodeAllClientsFailed
	case client.ErrorCategoryDownloadFailed:
		return exitCodeDownloadFailed
	case client.ErrorCategoryMP3TranscoderNotConfigured:
		return exitCodeMP3ConfigRequired
	case client.ErrorCategoryTranscriptParse:
		return exitCodeTranscriptParse
	default:
		return exitCodeGenericFailure
	}
}

func newDownloadArchive(path string) (*client.DownloadArchive, error) {
	return client.OpenDownloadArchive(path)
}
