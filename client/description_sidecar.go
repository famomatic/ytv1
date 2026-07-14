package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteDescriptionSidecar writes the video's description to outputPath.
func WriteDescriptionSidecar(info *VideoInfo, outputPath string) error {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return fmt.Errorf("%w: empty description output path", ErrInvalidInput)
	}
	description := ""
	if info != nil {
		description = info.Description
	}
	dir := filepath.Dir(outputPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create description directory: %w", err)
		}
	}
	if err := writeFileAtomic(outputPath, []byte(description), 0o644); err != nil {
		return fmt.Errorf("failed to write description: %w", err)
	}
	return nil
}

// writeFileAtomic writes data to a temp file in the same directory, fsyncs and
// closes it (surfacing flush errors), then renames it over outputPath so an
// interrupted write never leaves a partially written sidecar in place.
func writeFileAtomic(outputPath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(outputPath)
	tmp, err := os.CreateTemp(dir, ".sidecar-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, outputPath); err != nil {
		return err
	}
	committed = true
	return nil
}
