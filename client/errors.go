package client

import "errors"

var (
	// ErrInvalidInput indicates malformed input (not a video ID/url).
	ErrInvalidInput = errors.New("invalid input")
	// ErrUnavailable indicates video is unavailable.
	ErrUnavailable = errors.New("video unavailable")
	// ErrLoginRequired indicates authenticated session is required.
	ErrLoginRequired = errors.New("login required")
	// ErrNoPlayableFormats indicates no usable formats were found.
	ErrNoPlayableFormats = errors.New("no playable formats")
	// ErrChallengeNotSolved indicates URL deciphering is still required.
	ErrChallengeNotSolved = errors.New("challenge not solved")
	// ErrAllClientsFailed indicates fallback attempts all failed.
	ErrAllClientsFailed = errors.New("all clients failed")
)
