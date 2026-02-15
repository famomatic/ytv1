package client

import (
	"errors"
	"testing"

	"github.com/famomatic/ytv1/internal/orchestrator"
)

func TestMapErrorPlayabilityAgeRestricted(t *testing.T) {
	err := &orchestrator.PlayabilityError{
		Client: "WEB",
		Status: "LOGIN_REQUIRED",
		Reason: "This video may be inappropriate for some users.",
	}
	got := mapError(err)
	if !errors.Is(got, ErrLoginRequired) {
		t.Fatalf("mapError() = %v, want %v", got, ErrLoginRequired)
	}
	var detail *LoginRequiredDetailError
	if !errors.As(got, &detail) {
		t.Fatalf("mapError() should expose LoginRequiredDetailError")
	}
	if len(detail.Attempts) != 1 || detail.Attempts[0].Stage != "playability" {
		t.Fatalf("unexpected detail attempts: %+v", detail.Attempts)
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
	if got := mapError(err); !errors.Is(got, ErrUnavailable) {
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
	if got := mapError(err); !errors.Is(got, ErrLoginRequired) {
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
	got := mapError(err)
	if !errors.Is(got, ErrLoginRequired) {
		t.Fatalf("mapError() = %v, want %v", got, ErrLoginRequired)
	}
	var detail *LoginRequiredDetailError
	if !errors.As(got, &detail) {
		t.Fatalf("mapError() should expose LoginRequiredDetailError")
	}
	if len(detail.Attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(detail.Attempts))
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
	var detail *AllClientsFailedDetailError
	if !errors.As(mapError(err), &detail) {
		t.Fatalf("mapError() should expose AllClientsFailedDetailError")
	}
	if len(detail.Attempts) != 1 || detail.Attempts[0].Stage != "pot" {
		t.Fatalf("unexpected detail attempts: %+v", detail.Attempts)
	}
}
