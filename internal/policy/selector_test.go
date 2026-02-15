package policy

import (
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestDefaultOrderMatchesYTDLPStylePriority(t *testing.T) {
	s := NewSelector(innertube.NewRegistry(), nil, nil)
	profiles := s.Select("jNQXAC9IVRw")
	if len(profiles) < 8 {
		t.Fatalf("expected at least 8 profiles, got %d", len(profiles))
	}

	got := []string{
		profiles[0].Name,
		profiles[1].Name,
		profiles[2].Name,
		profiles[3].Name,
		profiles[4].Name,
		profiles[5].Name,
		profiles[6].Name,
		profiles[7].Name,
	}
	want := []string{"ANDROID_VR", "WEB", "WEB", "ANDROID", "IOS", "MWEB", "WEB_EMBEDDED_PLAYER", "TVHTML5"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOverridesAreNormalizedAndDeduplicated(t *testing.T) {
	s := NewSelector(innertube.NewRegistry(), []string{"  WEB ", "web", "mWeb", "unknown"}, nil)
	profiles := s.Select("jNQXAC9IVRw")
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "WEB" {
		t.Fatalf("first profile = %q, want WEB", profiles[0].Name)
	}
	if profiles[1].Name != "MWEB" {
		t.Fatalf("second profile = %q, want MWEB", profiles[1].Name)
	}
}

func TestOverrideAliasesAreAccepted(t *testing.T) {
	s := NewSelector(innertube.NewRegistry(), []string{"WEB_EMBEDDED_PLAYER", "TVHTML5"}, nil)
	profiles := s.Select("jNQXAC9IVRw")
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "WEB_EMBEDDED_PLAYER" {
		t.Fatalf("first profile = %q, want WEB_EMBEDDED_PLAYER", profiles[0].Name)
	}
	if profiles[1].Name != "TVHTML5" {
		t.Fatalf("second profile = %q, want TVHTML5", profiles[1].Name)
	}
}

func TestSkipClientsAreExcluded(t *testing.T) {
	s := NewSelector(innertube.NewRegistry(), []string{"web", "mweb", "ios"}, []string{"mweb"})
	profiles := s.Select("jNQXAC9IVRw")
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "WEB" || profiles[1].Name != "IOS" {
		t.Fatalf("unexpected order after skip: %q, %q", profiles[0].Name, profiles[1].Name)
	}
}
