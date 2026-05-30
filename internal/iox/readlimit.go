// Package iox provides I/O utility functions with safety bounds.
package iox

import (
	"errors"
	"fmt"
	"io"
)

// ErrBodyTooLarge is returned when a response body exceeds the configured read limit.
var ErrBodyTooLarge = errors.New("response body exceeds maximum allowed size")

// ReadAllLimit reads from r until EOF or until maxBytes+1 bytes have been read.
// If the underlying data exceeds maxBytes, ErrBodyTooLarge is returned and the
// partial data is discarded (not returned). This prevents unbounded memory
// allocation from malicious or malfunctioning servers.
func ReadAllLimit(r io.Reader, maxBytes int64) ([]byte, error) {
	// Read up to maxBytes+1 so we can detect overflow without losing data
	// in the exact-size case.
	lr := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrBodyTooLarge, int64(len(data)))
	}
	return data, nil
}
