package client

import "testing"

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
