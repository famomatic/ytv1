package client

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOutputOwnershipAndNoOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.mp4")
	release, err := acquireDownloadOutput(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := acquireDownloadOutput(path, false); err == nil {
		other()
		t.Fatal("two owners acquired the same output")
	}
	release()
	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if other, err := acquireDownloadOutput(path, true); !errors.Is(err, os.ErrExist) {
		if other != nil {
			other()
		}
		t.Fatalf("expected existing-file error: %v", err)
	}
	release, err = acquireDownloadOutput(path, false)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
