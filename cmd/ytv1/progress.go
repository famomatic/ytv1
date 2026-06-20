package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/famomatic/ytv1/client"
	"github.com/famomatic/ytv1/internal/cli"
)

type cliProgressPrinter struct {
	mu      sync.Mutex
	newline bool
	active  bool
}

func newCLIProgressPrinter(opts cli.Options) *cliProgressPrinter {
	return &cliProgressPrinter{newline: opts.NewlineProgress}
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
	if p.newline {
		fmt.Println(line)
		return
	}
	fmt.Printf("\r%-120s", line)
	p.active = true
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
	if evt.Total > 0 {
		percent = fmt.Sprintf("%5.1f%%", float64(evt.Downloaded)*100/float64(evt.Total))
	}
	total := "?"
	if evt.Total > 0 {
		total = formatBytes(evt.Total)
	}
	return fmt.Sprintf(
		"[download] %s %s at %s %s/%s",
		part,
		percent,
		formatMbps(evt.BytesPerSecond),
		formatBytes(evt.Downloaded),
		total,
	)
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
