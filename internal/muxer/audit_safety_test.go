package muxer

import (
	"context"
	"github.com/famomatic/ytv1/internal/types"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedMergePreservesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "existing.mp4")
	os.WriteFile(out, []byte("previous"), 0600)
	err := NewPureMuxMuxer(nil).Merge(context.Background(), filepath.Join(dir, "missing.video"), filepath.Join(dir, "missing.audio"), out, types.Metadata{})
	data, _ := os.ReadFile(out)
	if err == nil || string(data) != "previous" {
		t.Fatalf("merge=%v output=%q", err, data)
	}
}
