package challenge

import (
	"context"
	"sync/atomic"
	"testing"
)

type poProviderStub struct {
	token string
	calls int32
	empty bool
}

func (s *poProviderStub) GetToken(_ context.Context, _ string) (string, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.empty {
		return "", nil
	}
	return s.token, nil
}

func TestCachedPoTokenProvider_CachesByClient(t *testing.T) {
	base := &poProviderStub{token: "pot-1"}
	p := NewCachedPoTokenProvider(base)

	t1, err := p.GetToken(context.Background(), "WEB")
	if err != nil {
		t.Fatalf("first GetToken() error = %v", err)
	}
	t2, err := p.GetToken(context.Background(), "web")
	if err != nil {
		t.Fatalf("second GetToken() error = %v", err)
	}
	if t1 != "pot-1" || t2 != "pot-1" {
		t.Fatalf("unexpected token values: %q %q", t1, t2)
	}
	if got := atomic.LoadInt32(&base.calls); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestCachedPoTokenProvider_DoesNotCacheEmpty(t *testing.T) {
	base := &poProviderStub{empty: true}
	p := NewCachedPoTokenProvider(base)

	_, _ = p.GetToken(context.Background(), "web")
	_, _ = p.GetToken(context.Background(), "web")
	if got := atomic.LoadInt32(&base.calls); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
}
