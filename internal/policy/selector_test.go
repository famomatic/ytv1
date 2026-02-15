package policy

import (
	"testing"

	"github.com/mjmst/ytv1/internal/innertube"
)

func TestDefaultOrderIncludesMWeb(t *testing.T) {
	s := NewSelector(innertube.NewRegistry(), nil)
	profiles := s.Select("jNQXAC9IVRw")
	if len(profiles) < 6 {
		t.Fatalf("expected at least 6 profiles, got %d", len(profiles))
	}

	got := []string{
		profiles[0].Name,
		profiles[1].Name,
		profiles[2].Name,
		profiles[3].Name,
		profiles[4].Name,
		profiles[5].Name,
	}
	want := []string{"ANDROID", "IOS", "WEB", "MWEB", "WEB_EMBEDDED_PLAYER", "TVHTML5"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOverridesAreNormalizedAndDeduplicated(t *testing.T) {
	s := NewSelector(innertube.NewRegistry(), []string{"  WEB ", "web", "mWeb", "unknown"})
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
	s := NewSelector(innertube.NewRegistry(), []string{"WEB_EMBEDDED_PLAYER", "TVHTML5"})
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
