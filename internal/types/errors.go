package types

import "errors"

var (
	// ErrVideoUnavailable indicates that the video is unavailable (deleted, private, etc.).
	ErrVideoUnavailable = errors.New("video unavailable")

	// ErrLoginRequired indicates that the video requires login to view (e.g. premium content).
	ErrLoginRequired = errors.New("login required")

	// ErrAgeRestricted indicates that the video is age restricted and requires authentication or bypass.
	ErrAgeRestricted = errors.New("age restricted")

	// ErrNoClientsAvailable indicates that no clients were able to satisfy the request.
	ErrNoClientsAvailable = errors.New("no clients available")
)
