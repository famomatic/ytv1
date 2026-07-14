package client

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RemediationHintsForAttempts returns operator hints for failed extraction/download attempts.
func RemediationHintsForAttempts(attempts []AttemptDetail) []string {
	var hints []string
	sawLogin := false
	sawPOTRequired := false
	sawMissingPOT := false
	sawNoN := false
	sawHTTP403 := false
	sawHTTP429 := false

	for _, a := range attempts {
		if a.LoginRequired {
			sawLogin = true
		}
		if a.POTRequired {
			sawPOTRequired = true
			if !a.POTAvailable {
				sawMissingPOT = true
			}
		}
		if !a.URLHasN {
			sawNoN = true
		}
		if a.HTTPStatus == 403 {
			sawHTTP403 = true
		}
		if a.HTTPStatus == 429 {
			sawHTTP429 = true
		}
	}

	if sawLogin {
		hints = append(hints, "hint: login-required restriction detected. Retry with --cookies <netscape.txt> and, if needed, --visitor-data <VISITOR_INFO1_LIVE>.")
	}
	if sawPOTRequired && sawMissingPOT {
		hints = append(hints, "hint: missing required POT detected. Supply --po-token <token> or configure client.Config.PoTokenProvider.")
	}
	if sawHTTP429 {
		hints = append(hints, "hint: upstream throttling (HTTP 429). Retry later or use lower-concurrency network settings.")
	}
	if sawHTTP403 && sawNoN {
		hints = append(hints, "hint: 403 + missing n-signature observed. Retry with --verbose and verify [extract] challenge:success logs.")
	}
	if len(hints) == 0 {
		hints = append(hints, "hint: retry with --verbose --override-diagnostics to inspect client/stage-specific failure details.")
	}
	return hints
}

// GenericRemediationHints returns fallback operator hints for a classified error.
func GenericRemediationHints(err error) []string {
	var noPlayableDetail *NoPlayableFormatsDetailError
	switch {
	case errors.Is(err, ErrInvalidInput):
		return []string{"hint: unsupported input. Use a full YouTube URL or 11-char video ID, then retry."}
	case errors.Is(err, ErrLoginRequired):
		return []string{"hint: login-required content. Retry with --cookies <netscape.txt> and --visitor-data <VISITOR_INFO1_LIVE>."}
	case errors.Is(err, ErrNoPlayableFormats):
		if errors.As(err, &noPlayableDetail) && noPlayableDetail.Selector != "" {
			return []string{fmt.Sprintf("hint: selector %q matched no formats (%s). Retry with -F and adjust -f expression.", noPlayableDetail.Selector, noPlayableDetail.SelectionError)}
		}
		return []string{"hint: no playable formats. Retry with -F to inspect candidates and --verbose for extraction stages."}
	case errors.Is(err, ErrChallengeNotSolved):
		return []string{"hint: challenge solve failed. Retry with --verbose and inspect [extract] challenge:* logs."}
	case errors.Is(err, ErrMP3TranscoderNotConfigured):
		return []string{"hint: mp3 mode requires an MP3 transcoder. Configure client.Config.MP3Transcoder (CLI: use a build with transcoder wiring)."}
	default:
		return []string{"hint: retry with --verbose --override-diagnostics to inspect stage/client failure details."}
	}
}

// FormatExtractionEvent formats an extraction lifecycle event.
func FormatExtractionEvent(evt ExtractionEvent) string {
	scope := evt.Stage + ":" + evt.Phase
	if evt.Client != "" {
		scope += " client=" + evt.Client
	}
	if evt.Detail != "" {
		scope += " detail=" + evt.Detail
	}
	return "[extract] " + scope
}

// FormatDownloadEvent formats a download lifecycle event.
func FormatDownloadEvent(evt DownloadEvent) string {
	scope := evt.Stage + ":" + evt.Phase
	if evt.VideoID != "" {
		scope += " video_id=" + evt.VideoID
	}
	if evt.Path != "" {
		scope += " path=" + evt.Path
	}
	if evt.Detail != "" {
		scope += " detail=" + evt.Detail
	}
	return "[download] " + scope
}

// LifecyclePrinter formats lifecycle events with elapsed timing details.
type LifecyclePrinter struct {
	now func() time.Time
	mu  sync.Mutex

	extractStarts  map[string]time.Time
	downloadStarts map[string]time.Time
	videoTimings   map[string]VideoTiming
}

// NewLifecyclePrinter creates a lifecycle event formatter.
func NewLifecyclePrinter(now func() time.Time) *LifecyclePrinter {
	return &LifecyclePrinter{
		now:            now,
		extractStarts:  make(map[string]time.Time),
		downloadStarts: make(map[string]time.Time),
		videoTimings:   make(map[string]VideoTiming),
	}
}

// VideoTiming contains accumulated per-video lifecycle timing.
type VideoTiming struct {
	DownloadVideoMs  int64
	DownloadAudioMs  int64
	DownloadSingleMs int64
	MergeMs          int64
}

// FormatExtractionEvent formats an extraction event and records elapsed time.
func (p *LifecyclePrinter) FormatExtractionEvent(evt ExtractionEvent) string {
	detail := evt.Detail
	key := evt.Stage + "|" + evt.Client

	p.mu.Lock()
	switch evt.Phase {
	case "start":
		p.extractStarts[key] = p.now()
	case "success", "failure", "partial", "complete":
		if started, ok := p.extractStarts[key]; ok {
			detail = AppendDetail(detail, fmt.Sprintf("elapsed_ms=%d", p.now().Sub(started).Milliseconds()))
			delete(p.extractStarts, key)
		}
	}
	p.mu.Unlock()

	return FormatExtractionEvent(ExtractionEvent{Stage: evt.Stage, Phase: evt.Phase, Client: evt.Client, Detail: detail})
}

// FormatDownloadEvent formats a download event and records elapsed time.
func (p *LifecyclePrinter) FormatDownloadEvent(evt DownloadEvent) string {
	detail := evt.Detail
	key := evt.Stage + "|" + evt.VideoID + "|" + evt.Path
	now := p.now()

	p.mu.Lock()
	switch evt.Phase {
	case "start", "delete":
		p.downloadStarts[key] = now
	case "complete", "failure", "skip":
		if started, ok := p.downloadStarts[key]; ok {
			elapsed := now.Sub(started).Milliseconds()
			detail = AppendDetail(detail, fmt.Sprintf("elapsed_ms=%d", elapsed))
			if evt.Stage == "download" && evt.Phase == "complete" {
				if bytes, ok := ExtractBytesFromDetail(detail); ok && elapsed > 0 {
					seconds := float64(elapsed) / 1000.0
					speedBPS := float64(bytes) / seconds
					speedMiB := speedBPS / (1024.0 * 1024.0)
					detail = AppendDetail(detail, fmt.Sprintf("speed_bps=%d", int64(speedBPS)))
					detail = AppendDetail(detail, fmt.Sprintf("speed_mib_s=%.2f", speedMiB))
				}
				role := InferDownloadRole(evt.Path)
				detail = AppendDetail(detail, "part="+role)
				vt := p.videoTimings[evt.VideoID]
				switch role {
				case "video":
					vt.DownloadVideoMs += elapsed
				case "audio":
					vt.DownloadAudioMs += elapsed
				default:
					vt.DownloadSingleMs += elapsed
				}
				p.videoTimings[evt.VideoID] = vt
			}
			if evt.Stage == "merge" && evt.Phase == "complete" {
				vt := p.videoTimings[evt.VideoID]
				vt.MergeMs += elapsed
				p.videoTimings[evt.VideoID] = vt
			}
			delete(p.downloadStarts, key)
		}
	}
	p.mu.Unlock()

	return FormatDownloadEvent(DownloadEvent{Stage: evt.Stage, Phase: evt.Phase, VideoID: evt.VideoID, Path: evt.Path, Detail: detail})
}

// PopVideoTiming returns and clears accumulated timing for one video.
func (p *LifecyclePrinter) PopVideoTiming(videoID string) VideoTiming {
	p.mu.Lock()
	defer p.mu.Unlock()
	vt := p.videoTimings[videoID]
	delete(p.videoTimings, videoID)
	return vt
}

// AppendDetail appends a detail token if it is not already present.
func AppendDetail(base string, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return extra
	}
	// Match whole space-delimited tokens, not substrings: a substring check
	// wrongly suppresses e.g. "n=1" when base already contains "reason=1".
	for _, tok := range strings.Fields(base) {
		if tok == extra {
			return base
		}
	}
	return base + " " + extra
}

// ExtractBytesFromDetail parses bytes=N from a detail string.
func ExtractBytesFromDetail(detail string) (int64, bool) {
	tokens := strings.FieldsFunc(detail, func(r rune) bool {
		return r == ' ' || r == ','
	})
	for _, token := range tokens {
		if !strings.HasPrefix(token, "bytes=") {
			continue
		}
		raw := strings.TrimPrefix(token, "bytes=")
		v, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && v >= 0 {
			return v, true
		}
	}
	return 0, false
}

// InferDownloadRole classifies intermediate media file role from its suffix.
func InferDownloadRole(path string) string {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".video"):
		return "video"
	case strings.HasSuffix(lower, ".audio"):
		return "audio"
	default:
		return "single"
	}
}
