package client

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DownloadArchive stores completed video IDs for idempotent reruns.
type DownloadArchive struct {
	path string
	file *os.File
	mu   sync.Mutex
	ids  map[string]struct{}
}

// OpenDownloadArchive opens or creates a download archive file.
//
// Invalid/corrupt existing lines are ignored when loading, matching yt-dlp's
// forgiving archive behavior.
func OpenDownloadArchive(path string) (*DownloadArchive, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("archive path is empty")
	}
	if dir := filepath.Dir(cleanPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	archive := &DownloadArchive{
		path: cleanPath,
		file: f,
		ids:  make(map[string]struct{}),
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		key, err := archiveKey(line)
		if err != nil {
			continue
		}
		archive.ids[key] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return nil, err
	}
	return archive, nil
}

// Close closes the underlying archive file.
func (a *DownloadArchive) Close() error {
	if a == nil || a.file == nil {
		return nil
	}
	return a.file.Close()
}

// Has reports whether videoID is already recorded.
func (a *DownloadArchive) Has(videoID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key, err := archiveKey(videoID)
	if err != nil {
		return false
	}
	_, ok := a.ids[key]
	return ok
}

// Add records videoID if not already present.
func (a *DownloadArchive) Add(videoID string) error {
	if a == nil {
		return nil
	}
	key, err := archiveKey(videoID)
	if err != nil {
		return fmt.Errorf("invalid video id for archive: %q", videoID)
	}
	videoID = key
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.ids[videoID]; exists {
		return nil
	}
	if _, err := a.file.WriteString(videoID + "\n"); err != nil {
		return err
	}
	if err := a.file.Sync(); err != nil {
		return err
	}
	a.ids[videoID] = struct{}{}
	return nil
}

// ArchiveID returns a source-qualified archive key, preserving legacy YouTube IDs.
func ArchiveID(info *VideoInfo) string {
	if info == nil {
		return ""
	}
	if info.SourceName == "" || info.SourceName == "youtube" {
		return info.ID
	}
	return info.SourceName + " " + info.ID
}
func archiveKey(input string) (string, error) {
	fields := strings.Fields(input)
	if len(fields) == 2 {
		name, id := strings.ToLower(fields[0]), fields[1]
		if name == "youtube" {
			return ExtractVideoID(id)
		}
		valid := func(s string) bool {
			if s == "" {
				return false
			}
			for _, r := range s {
				if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
					return false
				}
			}
			return true
		}
		if valid(name) && valid(id) {
			return name + " " + id, nil
		}
	}
	return ExtractVideoID(input)
}
