package main

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
)

type cliProgressPrinter struct {
	mu          sync.Mutex
	newline     bool
	interactive bool
	active      bool
	lastLen     int
	completed   map[string]bool
	// baseTitle is the media title shown in the terminal window/tab title,
	// updated with a live percent as download progress events arrive.
	baseTitle string
}

func newCLIProgressPrinter(opts cli.Options) *cliProgressPrinter {
	return &cliProgressPrinter{
		newline:     opts.NewlineProgress,
		interactive: stdoutIsTerminal(),
		completed:   make(map[string]bool),
	}
}

func (p *cliProgressPrinter) Print(evt client.DownloadProgressEvent) {
	if p == nil {
		return
	}
	line := formatDownloadProgress(evt)
	if line == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.interactive {
		setConsoleTitle(consoleTitleText(p.baseTitle, evt))
	}
	key := evt.Path + "|" + evt.Part
	// Completion is the reporter's Final event or a true byte-total hit — NOT a
	// duration estimate reaching 100%, which routinely happens before the last
	// segment arrives (EXTINF sums exceed the rounded metadata duration). Using
	// the estimate froze the interactive bar and suppressed the real final line.
	complete := evt.Final || (evt.Total > 0 && evt.Downloaded >= evt.Total)
	if complete && p.completed[key] {
		return
	}
	if complete {
		p.completed[key] = true
	}
	if p.newline {
		fmt.Fprintln(statusW(), line)
		return
	}
	if !p.interactive {
		if complete {
			fmt.Fprintln(statusW(), line)
		}
		return
	}
	padding := ""
	if p.lastLen > len(line) {
		padding = strings.Repeat(" ", p.lastLen-len(line))
	}
	fmt.Fprintf(statusW(), "\r\033[2K%s%s", line, padding)
	p.active = true
	p.lastLen = len(line)
}

func (p *cliProgressPrinter) Finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active && !p.newline {
		if p.interactive {
			fmt.Fprint(statusW(), "\r\033[2K")
		} else {
			fmt.Fprintln(statusW())
		}
	}
	p.active = false
	p.lastLen = 0
}

// SetTitle records the media title for the terminal window/tab title and emits
// an initial title (no percent yet) so the terminal updates as soon as a
// download starts.
func (p *cliProgressPrinter) SetTitle(title string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.baseTitle = title
	interactive := p.interactive
	p.mu.Unlock()
	if interactive {
		setConsoleTitle(consoleTitleText(title, client.DownloadProgressEvent{}))
	}
}

// consoleTitleText builds the terminal title string: "<pct> <media> - ytv1",
// where the percent is derived from playback-duration progress (segmented
// HLS/DASH) or byte progress, and omitted when neither total is known.
func consoleTitleText(base string, evt client.DownloadProgressEvent) string {
	prefix := ""
	switch {
	case evt.TotalSeconds > 0:
		prefix = fmt.Sprintf("%.0f%% ", clampFraction(evt.DownloadedSeconds/evt.TotalSeconds)*100)
	case evt.Total > 0:
		prefix = fmt.Sprintf("%.0f%% ", clampFraction(float64(evt.Downloaded)/float64(evt.Total))*100)
	}
	base = strings.TrimSpace(base)
	if base == "" {
		title := prefix + "ytv1"
		return strings.TrimSpace(title)
	}
	return strings.TrimSpace(prefix + base + " - ytv1")
}

// setConsoleTitle sets the terminal window/tab title via the OSC 0 escape
// (icon + window title). Control characters are stripped so a crafted media
// title cannot inject further escape sequences.
func setConsoleTitle(title string) {
	if !stdoutIsTerminal() {
		return
	}
	title = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, title)
	fmt.Fprintf(os.Stdout, "\033]0;%s\007", title)
}

// resetConsoleTitle restores a neutral terminal title after a run so it does
// not stay stuck at a stale percent or media name.
func resetConsoleTitle() {
	setConsoleTitle("ytv1")
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func formatDownloadProgress(evt client.DownloadProgressEvent) string {
	// Duration mode: segmented HLS/DASH has no byte total but knows playback
	// seconds, so percent/bar/ETA come from duration while the size column shows
	// bytes-so-far over an unknown ("?") total.
	durationMode := evt.TotalSeconds > 0
	if evt.Downloaded <= 0 && evt.Total <= 0 && !durationMode {
		return ""
	}
	part := strings.TrimSpace(evt.Part)
	if part == "" {
		part = "media"
	}
	percent := "--.-%"
	fraction := float64(0)
	knownTotal := false
	switch {
	case durationMode:
		knownTotal = true
		fraction = clampFraction(evt.DownloadedSeconds / evt.TotalSeconds)
		percent = fmt.Sprintf("%5.1f%%", fraction*100)
	case evt.Total > 0:
		knownTotal = true
		fraction = clampFraction(float64(evt.Downloaded) / float64(evt.Total))
		percent = fmt.Sprintf("%5.1f%%", fraction*100)
	}
	total := "?"
	if !durationMode && evt.Total > 0 {
		total = formatBytes(evt.Total)
	}
	eta := formatETA(evt.Downloaded, evt.Total, evt.BytesPerSecond)
	if durationMode {
		eta = formatETASeconds(evt.ETASeconds)
	}
	return fmt.Sprintf(
		"[download] %-5s %s %s %s/%s %s eta %s",
		part,
		renderProgressBar(fraction, knownTotal, 18),
		percent,
		formatBytes(evt.Downloaded),
		total,
		formatMbps(evt.BytesPerSecond),
		eta,
	)
}

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// formatETASeconds renders a precomputed seconds-remaining value; -1 is unknown.
func formatETASeconds(seconds int64) string {
	if seconds < 0 {
		return "--:--"
	}
	minutes := seconds / 60
	seconds = seconds % 60
	if minutes > 99 {
		return ">99m"
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func renderProgressBar(fraction float64, knownTotal bool, width int) string {
	if width <= 0 {
		width = 1
	}
	if !knownTotal {
		return "[" + strings.Repeat("-", width) + "]"
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func formatBytes(v int64) string {
	if v < 0 {
		v = 0
	}
	const unit = 1024.0
	value := float64(v)
	units := []string{"B", "KiB", "MiB", "GiB"}
	idx := 0
	for value >= unit && idx < len(units)-1 {
		value /= unit
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%d%s", v, units[idx])
	}
	return fmt.Sprintf("%.1f%s", value, units[idx])
}

func formatMbps(bytesPerSecond int64) string {
	if bytesPerSecond <= 0 {
		return "0.00Mbps"
	}
	return fmt.Sprintf("%.2fMbps", float64(bytesPerSecond)*8/1000/1000)
}

func formatETA(downloaded, total, bytesPerSecond int64) string {
	if total <= 0 || bytesPerSecond <= 0 || downloaded >= total {
		return "--:--"
	}
	remaining := total - downloaded
	seconds := remaining / bytesPerSecond
	if remaining%bytesPerSecond != 0 {
		seconds++
	}
	if seconds < 0 {
		seconds = 0
	}
	minutes := seconds / 60
	seconds = seconds % 60
	if minutes > 99 {
		return ">99m"
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}
