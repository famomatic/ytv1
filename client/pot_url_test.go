package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/famomatic/ytv1/internal/innertube"
	"github.com/famomatic/ytv1/internal/orchestrator"
)

type tokenProviderStub struct {
	token string
	calls int32
}

func (s *tokenProviderStub) GetToken(_ context.Context, _ string) (string, error) {
	atomic.AddInt32(&s.calls, 1)
	return s.token, nil
}

func TestApplyPoTokenPolicyToURL_InjectsAndCachesToken(t *testing.T) {
	stub := &tokenProviderStub{token: "pot-123"}
	c := New(Config{
		PoTokenProvider: stub,
		PoTokenFetchPolicy: map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy{
			innertube.StreamingProtocolHTTPS: innertube.PoTokenFetchPolicyRequired,
		},
	})

	u1, err := c.applyPoTokenPolicyToURL(context.Background(), "https://media.example/v.webm?itag=248", "web", innertube.StreamingProtocolHTTPS)
	if err != nil {
		t.Fatalf("first applyPoTokenPolicyToURL() error = %v", err)
	}
	u2, err := c.applyPoTokenPolicyToURL(context.Background(), "https://media.example/v.webm?itag=248", "web", innertube.StreamingProtocolHTTPS)
	if err != nil {
		t.Fatalf("second applyPoTokenPolicyToURL() error = %v", err)
	}
	if u1 != "https://media.example/v.webm?itag=248&pot=pot-123" || u2 != u1 {
		t.Fatalf("unexpected rewritten urls: %q %q", u1, u2)
	}
	if got := atomic.LoadInt32(&stub.calls); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestApplyPoTokenPolicyToURL_RequiredFailsWithoutProvider(t *testing.T) {
	c := New(Config{
		PoTokenFetchPolicy: map[innertube.VideoStreamingProtocol]innertube.PoTokenFetchPolicy{
			innertube.StreamingProtocolHTTPS: innertube.PoTokenFetchPolicyRequired,
		},
	})

	_, err := c.applyPoTokenPolicyToURL(context.Background(), "https://media.example/v.webm?itag=248", "web", innertube.StreamingProtocolHTTPS)
	if err == nil {
		t.Fatalf("expected error")
	}
	var potErr *orchestrator.PoTokenRequiredError
	if !errors.As(err, &potErr) {
		t.Fatalf("expected PoTokenRequiredError, got %T", err)
	}
}
