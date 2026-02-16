package cli

import (
	"context"
	"testing"
)

func TestToClientConfig_StaticPoTokenProvider(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		PoToken: "token-abc",
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.PoTokenProvider == nil {
		t.Fatalf("expected PoTokenProvider to be configured")
	}
	token, err := cfg.PoTokenProvider.GetToken(context.Background(), "web")
	if err != nil {
		t.Fatalf("PoTokenProvider.GetToken() error = %v", err)
	}
	if token != "token-abc" {
		t.Fatalf("token = %q, want %q", token, "token-abc")
	}
}

func TestToClientConfig_EmptyPoTokenDoesNotConfigureProvider(t *testing.T) {
	cfg, err := ToClientConfig(Options{
		PoToken: "   ",
	})
	if err != nil {
		t.Fatalf("ToClientConfig() error = %v", err)
	}
	if cfg.PoTokenProvider != nil {
		t.Fatalf("expected PoTokenProvider to be nil for empty override")
	}
}
