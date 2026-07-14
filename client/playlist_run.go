package client

import (
	"context"
	"fmt"
	"strings"
)

// PlaylistRunSummary describes playlist item processing outcomes.
type PlaylistRunSummary struct {
	Total     int
	Succeeded int
	Failed    int
	Aborted   bool
	Skipped   int
	// AbortErr carries the error that caused an intentional abort
	// (context cancellation, break-on-existing, max-downloads reached) so the
	// caller can propagate it instead of treating the run as a plain success.
	AbortErr error
}

// PlaylistItemFailure describes a failed playlist item.
type PlaylistItemFailure struct {
	VideoID string
	Err     error
}

// PlaylistRunOptions controls package-level playlist item processing.
type PlaylistRunOptions struct {
	AbortOnError          bool
	SkipAfterErrors       int
	IsBreakOnExisting     func(error) bool
	IsMaxDownloadsReached func(error) bool
	OnItemStart           func(index int, total int, item PlaylistItem)
}

// PlaylistItemProcessor processes one selected playlist item.
type PlaylistItemProcessor func(context.Context, PlaylistItem, int) error

// RunPlaylistItems processes playlist items with deterministic summary,
// failure, abort, and skipped-count accounting.
func RunPlaylistItems(ctx context.Context, items []PlaylistItem, opts PlaylistRunOptions, processor PlaylistItemProcessor) (PlaylistRunSummary, []PlaylistItemFailure) {
	summary := PlaylistRunSummary{Total: len(items)}
	failures := make([]PlaylistItemFailure, 0)
	for i, item := range items {
		// Honor cancellation before starting each item so a Ctrl-C on a large
		// playlist surfaces the cancellation instead of marking every remaining
		// item "failed" and spinning the processor once per item.
		if err := ctx.Err(); err != nil {
			summary.Aborted = true
			summary.AbortErr = err
			summary.Skipped = len(items) - i
			break
		}
		if opts.OnItemStart != nil {
			opts.OnItemStart(i+1, len(items), item)
		}
		if err := processor(ctx, item, i+1); err != nil {
			if opts.IsBreakOnExisting != nil && opts.IsBreakOnExisting(err) {
				summary.Aborted = true
				summary.AbortErr = err
				summary.Skipped = len(items) - i - 1
				break
			}
			if opts.IsMaxDownloadsReached != nil && opts.IsMaxDownloadsReached(err) {
				summary.Succeeded++
				summary.Aborted = true
				summary.AbortErr = err
				summary.Skipped = len(items) - i - 1
				break
			}
			summary.Failed++
			failures = append(failures, PlaylistItemFailure{VideoID: item.VideoID, Err: err})
			if opts.AbortOnError || ReachedPlaylistFailureThreshold(summary.Failed, opts.SkipAfterErrors) {
				summary.Aborted = true
				summary.Skipped = len(items) - i - 1
				break
			}
			continue
		}
		summary.Succeeded++
	}
	return summary, failures
}

// ReachedPlaylistFailureThreshold reports whether playlist processing should stop.
func ReachedPlaylistFailureThreshold(failures int, threshold int) bool {
	return threshold > 0 && failures >= threshold
}

// PlaylistTemplateContext contains playlist metadata used for output templates.
type PlaylistTemplateContext struct {
	ID         string
	Title      string
	Uploader   string
	UploaderID string
	Channel    string
	ChannelID  string
	Count      int
}

// ApplyPlaylistTemplateContext renders playlist-level output template tokens.
func ApplyPlaylistTemplateContext(outputTemplate string, item PlaylistItem, autonumber int, playlistCtx PlaylistTemplateContext) string {
	if strings.TrimSpace(outputTemplate) == "" {
		return outputTemplate
	}
	index := item.PlaylistIndex
	if index <= 0 {
		index = autonumber
	}
	replacements := map[string]string{
		"%(playlist_index)s":       formatPaddedInt(index),
		"%(playlist_autonumber)s":  formatPaddedInt(autonumber),
		"%(playlist_count)s":       formatPaddedInt(playlistCtx.Count),
		"%(playlist_id)s":          SanitizeOutputTemplateToken(playlistCtx.ID),
		"%(playlist_title)s":       SanitizeOutputTemplateToken(playlistCtx.Title),
		"%(playlist_uploader)s":    SanitizeOutputTemplateToken(playlistCtx.Uploader),
		"%(playlist_uploader_id)s": SanitizeOutputTemplateToken(playlistCtx.UploaderID),
		"%(playlist_channel)s":     SanitizeOutputTemplateToken(playlistCtx.Channel),
		"%(playlist_channel_id)s":  SanitizeOutputTemplateToken(playlistCtx.ChannelID),
	}
	for token, value := range replacements {
		outputTemplate = strings.ReplaceAll(outputTemplate, token, value)
	}
	return outputTemplate
}

func formatPaddedInt(v int) string {
	return fmt.Sprintf("%05d", v)
}
