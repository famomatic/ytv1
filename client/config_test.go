package client

import (
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
)

func TestToInnerTubeConfig_DisablesFallbackInOverrideModeByDefault(t *testing.T) {
	cfg := Config{
		ClientOverrides: []string{"android_vr", "web", "web_safari"},
	}
	inner := cfg.ToInnerTubeConfig()
	if !inner.DisableFallbackClients {
		t.Fatalf("expected DisableFallbackClients=true when ClientOverrides is set")
	}
}

func TestToInnerTubeConfig_AllowsFallbackInOverrideModeWhenOptedIn(t *testing.T) {
	cfg := Config{
		ClientOverrides:                 []string{"android_vr", "web", "web_safari"},
		AppendFallbackOnClientOverrides: true,
	}
	inner := cfg.ToInnerTubeConfig()
	if inner.DisableFallbackClients {
		t.Fatalf("expected DisableFallbackClients=false when AppendFallbackOnClientOverrides=true")
	}
}

func TestToInnerTubeConfig_ExplicitDisableFallbackStillWins(t *testing.T) {
	cfg := Config{
		ClientOverrides:                 []string{"android_vr", "web", "web_safari"},
		AppendFallbackOnClientOverrides: true,
		DisableFallbackClients:          true,
	}
	inner := cfg.ToInnerTubeConfig()
	if !inner.DisableFallbackClients {
		t.Fatalf("expected DisableFallbackClients=true when explicitly configured")
	}
}

func TestToInnerTubeConfig_MapsExtractionEventHandler(t *testing.T) {
	var called bool
	cfg := Config{
		OnExtractionEvent: func(evt ExtractionEvent) {
			called = evt.Stage == "player_api_json" && evt.Phase == "success" && evt.Client == "WEB"
		},
	}
	inner := cfg.ToInnerTubeConfig()
	if inner.OnExtractionEvent == nil {
		t.Fatalf("expected OnExtractionEvent to be mapped")
	}
	inner.OnExtractionEvent(innertube.ExtractionEvent{
		Stage:  "player_api_json",
		Phase:  "success",
		Client: "WEB",
	})
	if !called {
		t.Fatalf("expected mapped handler to be called")
	}
}
