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
	// ErrMP3TranscoderNotConfigured indicates mp3 mode was requested without a transcoder.
	ErrMP3TranscoderNotConfigured = errors.New("mp3 transcoder not configured")
	// ErrTranscriptParse indicates transcript payload could not be parsed.
	ErrTranscriptParse = errors.New("transcript parse failed")
)

// InvalidInputDetailError preserves ErrInvalidInput while exposing parsing reason/context.
type InvalidInputDetailError struct {
	Input  string
	Reason string
}

// Error returns a human-readable invalid input reason.
func (e *InvalidInputDetailError) Error() string {
	return "invalid input: " + e.Reason
}

// Is reports sentinel compatibility with ErrInvalidInput.
func (e *InvalidInputDetailError) Is(target error) bool {
	return target == ErrInvalidInput
}

// MP3TranscoderError provides mode/context detail while preserving sentinel matching.
type MP3TranscoderError struct {
	Mode SelectionMode
}

// Error returns a human-readable MP3 transcoder configuration error.
func (e *MP3TranscoderError) Error() string {
	return "mp3 transcoder not configured for mode=" + string(e.Mode)
}

// Is reports sentinel compatibility with ErrMP3TranscoderNotConfigured.
func (e *MP3TranscoderError) Is(target error) bool {
	return target == ErrMP3TranscoderNotConfigured
}

// FormatSkipReason captures why a candidate format was dropped.
type FormatSkipReason struct {
	Itag     int
	Protocol string
	Reason   string
}

// NoPlayableFormatsDetailError preserves ErrNoPlayableFormats while exposing skip details.
type NoPlayableFormatsDetailError struct {
	Mode  SelectionMode
	Skips []FormatSkipReason
}

// Error returns a summary of the no-playable-formats condition.
func (e *NoPlayableFormatsDetailError) Error() string {
	return "no playable formats after filtering for mode=" + string(e.Mode)
}

// Is reports sentinel compatibility with ErrNoPlayableFormats.
func (e *NoPlayableFormatsDetailError) Is(target error) bool {
	return target == ErrNoPlayableFormats
}

// AttemptDetail captures a single client attempt in the fallback matrix.
type AttemptDetail struct {
	Client               string
	Stage                string
	Reason               string
	HTTPStatus           int
	Itag                 int
	Protocol             string
	URLHost              string
	URLHasN              bool
	URLHasPOT            bool
	URLHasSignature      bool
	POTRequired          bool
	POTAvailable         bool
	POTPolicy            string
	POTProtocols         []string
	PlayabilityStatus    string
	PlayabilityReason    string
	PlayabilitySubreason string
	GeoRestricted        bool
	LoginRequired        bool
	AgeRestricted        bool
	Unavailable          bool
	DRMProtected         bool
	AvailableCountries   []string
}

// DownloadFailureDetailError preserves download failure context while exposing attempt-style diagnostics.
type DownloadFailureDetailError struct {
	Attempts []AttemptDetail
}

// Error returns a summary of download failure details.
func (e *DownloadFailureDetailError) Error() string {
	return "download failed with detailed attempts"
}

// AllClientsFailedDetailError preserves ErrAllClientsFailed while exposing attempt details.
type AllClientsFailedDetailError struct {
	Attempts []AttemptDetail
}

// Error returns a summary of all-client failure.
func (e *AllClientsFailedDetailError) Error() string {
	return "all clients failed with detailed attempts"
}

// Is reports sentinel compatibility with ErrAllClientsFailed.
func (e *AllClientsFailedDetailError) Is(target error) bool {
	return target == ErrAllClientsFailed
}

// LoginRequiredDetailError preserves ErrLoginRequired while exposing attempt details.
type LoginRequiredDetailError struct {
	Attempts []AttemptDetail
}

// Error returns a summary of login-required failure.
func (e *LoginRequiredDetailError) Error() string {
	return "login required with detailed attempts"
}

// Is reports sentinel compatibility with ErrLoginRequired.
func (e *LoginRequiredDetailError) Is(target error) bool {
	return target == ErrLoginRequired
}

// UnavailableDetailError preserves ErrUnavailable while exposing attempt details.
type UnavailableDetailError struct {
	Attempts []AttemptDetail
}

// Error returns a summary of unavailable-content failure.
func (e *UnavailableDetailError) Error() string {
	return "video unavailable with detailed attempts"
}

// Is reports sentinel compatibility with ErrUnavailable.
func (e *UnavailableDetailError) Is(target error) bool {
	return target == ErrUnavailable
}

// TranscriptUnavailableDetailError preserves ErrUnavailable with transcript context.
type TranscriptUnavailableDetailError struct {
	VideoID      string
	LanguageCode string
	Reason       string
}

// Error returns a human-readable transcript unavailable reason.
func (e *TranscriptUnavailableDetailError) Error() string {
	return "transcript unavailable: " + e.Reason
}

// Is reports sentinel compatibility with ErrUnavailable.
func (e *TranscriptUnavailableDetailError) Is(target error) bool {
	return target == ErrUnavailable
}

// TranscriptParseDetailError preserves ErrTranscriptParse with payload context.
type TranscriptParseDetailError struct {
	VideoID      string
	LanguageCode string
	Reason       string
}

// Error returns a human-readable transcript parse failure reason.
func (e *TranscriptParseDetailError) Error() string {
	return "transcript parse failed: " + e.Reason
}

// Is reports sentinel compatibility with ErrTranscriptParse.
func (e *TranscriptParseDetailError) Is(target error) bool {
	return target == ErrTranscriptParse
}

// AttemptDetails extracts attempt matrix details from typed package errors.
func AttemptDetails(err error) ([]AttemptDetail, bool) {
	if err == nil {
		return nil, false
	}
	var allErr *AllClientsFailedDetailError
	if errors.As(err, &allErr) {
		return allErr.Attempts, true
	}
	var loginErr *LoginRequiredDetailError
	if errors.As(err, &loginErr) {
		return loginErr.Attempts, true
	}
	var unavailableErr *UnavailableDetailError
	if errors.As(err, &unavailableErr) {
		return unavailableErr.Attempts, true
	}
	var downloadErr *DownloadFailureDetailError
	if errors.As(err, &downloadErr) {
		return downloadErr.Attempts, true
	}
	return nil, false
}
