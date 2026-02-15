package client

import (
	"errors"
	"testing"

	"github.com/mjmst/ytv1/internal/orchestrator"
)

func TestMapErrorPlayabilityAgeRestricted(t *testing.T) {
	err := &orchestrator.PlayabilityError{
		Client: "WEB",
		Status: "LOGIN_REQUIRED",
		Reason: "This video may be inappropriate for some users.",
	}
	if got := mapError(err); got != ErrLoginRequired {
		t.Fatalf("mapError() = %v, want %v", got, ErrLoginRequired)
	}
}

func TestMapErrorAllClientsFailedUnavailable(t *testing.T) {
	err := &orchestrator.AllClientsFailedError{
		Attempts: []orchestrator.AttemptError{
			{
				Client: "WEB",
				Err: &orchestrator.PlayabilityError{
					Client: "WEB",
					Status: "UNPLAYABLE",
					Reason: "The uploader has not made this video available in your country",
				},
			},
		},
	}
	if got := mapError(err); got != ErrUnavailable {
		t.Fatalf("mapError() = %v, want %v", got, ErrUnavailable)
	}
}

func TestMapErrorAllClientsFailedLogin(t *testing.T) {
	err := &orchestrator.AllClientsFailedError{
		Attempts: []orchestrator.AttemptError{
			{
				Client: "IOS",
				Err: &orchestrator.PlayabilityError{
					Client: "IOS",
					Status: "LOGIN_REQUIRED",
					Reason: "Sign in to confirm your age",
				},
			},
		},
	}
	if got := mapError(err); got != ErrLoginRequired {
		t.Fatalf("mapError() = %v, want %v", got, ErrLoginRequired)
	}
}

func TestMapErrorMixedFailureMatrixPrefersLogin(t *testing.T) {
	err := &orchestrator.AllClientsFailedError{
		Attempts: []orchestrator.AttemptError{
			{
				Client: "WEB",
				Err: &orchestrator.PoTokenRequiredError{
					Client: "WEB",
					Cause:  "provider not configured",
				},
			},
			{
				Client: "MWEB",
				Err: &orchestrator.HTTPStatusError{
					Client:     "MWEB",
					StatusCode: 502,
				},
			},
			{
				Client: "IOS",
				Err: &orchestrator.PlayabilityError{
					Client: "IOS",
					Status: "LOGIN_REQUIRED",
					Reason: "Sign in to confirm your age",
				},
			},
		},
	}
	if got := mapError(err); got != ErrLoginRequired {
		t.Fatalf("mapError() = %v, want %v", got, ErrLoginRequired)
	}
}

func TestMapErrorPoTokenRequiredFallsBackToAllClientsFailed(t *testing.T) {
	err := &orchestrator.PoTokenRequiredError{
		Client: "WEB",
		Cause:  "provider not configured",
	}
	if got := mapError(err); !errors.Is(got, ErrAllClientsFailed) {
		t.Fatalf("mapError() = %v, want %v", got, ErrAllClientsFailed)
	}
}
