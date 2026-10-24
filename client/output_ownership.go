package client

import (
	"fmt"
	"os"
)

// acquireDownloadOutput excludes other ytv1 processes before touching either
// the output or its resumable parts. A lock left by a crashed process is kept
// deliberately: automatically stealing it could corrupt a running download.
func acquireDownloadOutput(path string, noOverwrites bool) (func(), error) {
	if path == "-" { return func() {}, nil }
	lockPath := path + ".ytv1-lock"
	if err := os.Mkdir(lockPath, 0700); err != nil {
		return nil, fmt.Errorf("acquire output %q (remove stale %q only after its owner has stopped): %w", path, lockPath, err)
	}
	release := func() { _ = os.Remove(lockPath) }
	if noOverwrites {
		if _, err := os.Lstat(path); err == nil {
			release()
			return nil, fmt.Errorf("output %q already exists: %w", path, os.ErrExist)
		} else if !os.IsNotExist(err) { release(); return nil, err }
	}
	return release, nil
}
