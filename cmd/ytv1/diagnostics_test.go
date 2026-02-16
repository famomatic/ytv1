package main

import (
	"strings"
	"testing"

	"github.com/famomatic/ytv1/client"
)

func TestRemediationHintsForAttempts_MissingPOT(t *testing.T) {
	hints := remediationHintsForAttempts([]client.AttemptDetail{
		{
			POTRequired:  true,
			POTAvailable: false,
		},
	})
	if len(hints) == 0 {
		t.Fatalf("expected hints for missing POT")
	}
	found := false
	for _, h := range hints {
		if strings.Contains(h, "--po-token") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected --po-token hint, got: %v", hints)
	}
}

func TestRemediationHintsForAttempts_403AndNoN(t *testing.T) {
	hints := remediationHintsForAttempts([]client.AttemptDetail{
		{
			HTTPStatus: 403,
			URLHasN:    false,
		},
	})
	found := false
	for _, h := range hints {
		if strings.Contains(h, "missing n-signature") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected n-signature hint, got: %v", hints)
	}
}
