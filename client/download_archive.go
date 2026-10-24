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
	discardPartial bool
	readOffset     int64
	path           string
	file           *os.File
	mu             sync.Mutex
	ids            map[string]struct{}
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
	if err := archive.refreshLocked(true); err != nil {
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
	_ = a.refreshLocked(false)
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
	if err := a.refreshLocked(false); err != nil {
		return err
	}
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

// Read only newly appended records. Oversized corrupt records are drained in
// bounded slices so a bad line cannot disable the complete archive.
func (a *DownloadArchive) refreshLocked(acceptFinal bool) error {
	f, err := os.Open(a.path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if stat.Size() < a.readOffset {
		a.readOffset = 0
		a.discardPartial = false
		a.ids = make(map[string]struct{})
	}
	if _, err = f.Seek(a.readOffset, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReader(f)
	var line []byte
	oversized := a.discardPartial
	for {
		part, err := r.ReadSlice('\n')
		a.readOffset += int64(len(part))
		if len(line)+len(part) > 64*1024 {
			oversized = true
			line = nil
		}
		if !oversized {
			line = append(line, part...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF && len(line) > 0 && !acceptFinal {
			a.readOffset -= int64(len(line))
			return nil
		}
		if !oversized {
			if key, e := archiveKey(strings.TrimSpace(string(line))); e == nil {
				a.ids[key] = struct{}{}
			}
		}
		if err == io.EOF {
			a.discardPartial = oversized
			return nil
		}
		line = nil
		oversized = false
		if err != nil {
			return err
		}
	}
}
