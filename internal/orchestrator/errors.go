package orchestrator

import (
	"fmt"
	"strings"
)

// AttemptError captures one client attempt failure.
type AttemptError struct {
	Client string
	Err    error
}

// AllClientsFailedError is returned when no client attempt succeeded.
type AllClientsFailedError struct {
	Attempts []AttemptError
}

func (e *AllClientsFailedError) Error() string {
	if len(e.Attempts) == 0 {
		return "all clients failed"
	}
	return fmt.Sprintf("all clients failed: %d attempt(s)", len(e.Attempts))
}

// HTTPStatusError indicates non-200 Innertube response.
type HTTPStatusError struct {
	Client     string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("innertube http status=%d client=%s", e.StatusCode, e.Client)
}

// PlayabilityError indicates an unplayable player response.
type PlayabilityError struct {
	Client string
	Status string
	Reason string
}

func (e *PlayabilityError) Error() string {
	return fmt.Sprintf("unplayable status=%s client=%s reason=%s", e.Status, e.Client, e.Reason)
}

func (e *PlayabilityError) RequiresLogin() bool {
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "LOGIN") || strings.Contains(s, "SIGN IN")
}

func (e *PlayabilityError) IsAgeRestricted() bool {
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "AGE")
}

func (e *PlayabilityError) IsGeoRestricted() bool {
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "COUNTRY") ||
		strings.Contains(s, "REGION") ||
		strings.Contains(s, "LOCATION")
}

func (e *PlayabilityError) IsUnavailable() bool {
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "UNAVAILABLE") ||
		strings.Contains(s, "PRIVATE") ||
		strings.Contains(s, "DELETED")
}

// PoTokenRequiredError indicates a request could not proceed due to missing/invalid PO token.
type PoTokenRequiredError struct {
	Client string
	Cause  string
}

func (e *PoTokenRequiredError) Error() string {
	return fmt.Sprintf("po token required client=%s cause=%s", e.Client, e.Cause)
}
