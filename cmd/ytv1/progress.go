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
	key := evt.Path + "|" + evt.Part
	complete := evt.Total > 0 && evt.Downloaded >= evt.Total
	if complete && p.completed[key] {
		return
	}
	if complete {
		p.completed[key] = true
	}
	if p.newline {
		fmt.Println(line)
		return
	}
	if !p.interactive {
		if complete {
			fmt.Println(line)
		}
		return
	}
	padding := ""
	if p.lastLen > len(line) {
		padding = strings.Repeat(" ", p.lastLen-len(line))
	}
	fmt.Printf("\r\033[2K%s%s", line, padding)
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
		fmt.Println()
	}
	p.active = false
	p.lastLen = 0
}

func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func formatDownloadProgress(evt client.DownloadProgressEvent) string {
	if evt.Downloaded <= 0 && evt.Total <= 0 {
		return ""
	}
	part := strings.TrimSpace(evt.Part)
	if part == "" {
		part = "media"
	}
	percent := "--.-%"
	fraction := float64(0)
	if evt.Total > 0 {
		fraction = float64(evt.Downloaded) / float64(evt.Total)
		if fraction < 0 {
			fraction = 0
		}
		if fraction > 1 {
			fraction = 1
		}
		percent = fmt.Sprintf("%5.1f%%", fraction*100)
	}
	total := "?"
	if evt.Total > 0 {
		total = formatBytes(evt.Total)
	}
	return fmt.Sprintf(
		"[download] %-5s %s %s %s/%s %s eta %s",
		part,
		renderProgressBar(fraction, evt.Total > 0, 18),
		percent,
		formatBytes(evt.Downloaded),
		total,
		formatMbps(evt.BytesPerSecond),
		formatETA(evt.Downloaded, evt.Total, evt.BytesPerSecond),
	)
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
