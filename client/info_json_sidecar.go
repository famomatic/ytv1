package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteInfoJSONSidecar writes a yt-dlp-style single-video .info.json sidecar.
func WriteInfoJSONSidecar(input string, info *VideoInfo, outputPath string) error {
	if err := mkdirParent(outputPath, "info json"); err != nil {
		return err
	}
	if err := writeJSONAtomic(outputPath, BuildYTDLPDumpSingleJSON(input, info)); err != nil {
		return fmt.Errorf("failed to write info json: %w", err)
	}
	return nil
}

// WritePlaylistInfoJSONSidecar writes a yt-dlp-style playlist .info.json sidecar.
func WritePlaylistInfoJSONSidecar(playlist *PlaylistInfo, outputPath string) error {
	if err := mkdirParent(outputPath, "playlist info json"); err != nil {
		return err
	}
	if err := writeJSONAtomic(outputPath, BuildYTDLPPlaylistInfoJSON(playlist)); err != nil {
		return fmt.Errorf("failed to write playlist info json: %w", err)
	}
	return nil
}

// writeJSONAtomic encodes v as indented JSON to a temp file in the same
// directory, fsyncs and closes it (surfacing any flush error), then renames it
// over outputPath. This avoids truncating a good existing sidecar on a
// mid-encode failure and avoids silently reporting success when a buffered
// write never reaches disk (disk-full / network FS).
func writeJSONAtomic(outputPath string, v any) error {
	dir := filepath.Dir(outputPath)
	tmp, err := os.CreateTemp(dir, ".info-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if we return before a successful rename.
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, outputPath); err != nil {
		return err
	}
	committed = true
	return nil
}

func mkdirParent(outputPath string, label string) error {
	dir := filepath.Dir(outputPath)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", label, err)
	}
	return nil
}
