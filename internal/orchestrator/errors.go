package orchestrator

import (
	"fmt"
	"strings"

	"github.com/famomatic/ytv1/internal/innertube"
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
	Detail PlayabilityDetail
}

func (e *PlayabilityError) Error() string {
	return fmt.Sprintf("unplayable status=%s client=%s reason=%s", e.Status, e.Client, e.Reason)
}

// PlayabilityDetail keeps typed classification data from player response.
type PlayabilityDetail struct {
	Subreason          string
	AvailableCountries []string
	GeoRestricted      bool
	LoginRequired      bool
	AgeRestricted      bool
	Unavailable        bool
	DRMProtected       bool
}

func (e *PlayabilityError) RequiresLogin() bool {
	if e.Detail.LoginRequired {
		return true
	}
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "LOGIN") || strings.Contains(s, "SIGN IN")
}

func (e *PlayabilityError) IsAgeRestricted() bool {
	if e.Detail.AgeRestricted {
		return true
	}
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "AGE")
}

func (e *PlayabilityError) IsGeoRestricted() bool {
	if e.Detail.GeoRestricted {
		return true
	}
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "COUNTRY") ||
		strings.Contains(s, "REGION") ||
		strings.Contains(s, "LOCATION")
}

func (e *PlayabilityError) IsUnavailable() bool {
	if e.Detail.Unavailable {
		return true
	}
	s := strings.ToUpper(e.Status + " " + e.Reason)
	return strings.Contains(s, "UNAVAILABLE") ||
		strings.Contains(s, "PRIVATE") ||
		strings.Contains(s, "DELETED")
}

func (e *PlayabilityError) IsDRMProtected() bool {
	if e.Detail.DRMProtected {
		return true
	}
	s := strings.ToUpper(e.Status + " " + e.Reason + " " + e.Detail.Subreason)
	return strings.Contains(s, "DRM")
}

// PoTokenRequiredError indicates a request could not proceed due to missing/invalid PO token.
type PoTokenRequiredError struct {
	Client            string
	Cause             string
	Policy            innertube.PoTokenFetchPolicy
	Protocols         []innertube.VideoStreamingProtocol
	ProviderAvailable bool
}

func (e *PoTokenRequiredError) Error() string {
	parts := make([]string, 0, len(e.Protocols))
	for _, p := range e.Protocols {
		parts = append(parts, string(p))
	}
	return fmt.Sprintf(
		"po token required client=%s policy=%s protocols=%s provider_available=%t cause=%s",
		e.Client,
		e.Policy,
		strings.Join(parts, ","),
		e.ProviderAvailable,
		e.Cause,
	)
}
