package challenge

import (
	"context"
	"errors"
	"testing"
)

type deciphererStub struct {
	n   map[string]string
	sig map[string]string
}

func (d deciphererStub) DecipherN(challenge string) (string, error) {
	if out, ok := d.n[challenge]; ok {
		return out, nil
	}
	return "", errors.New("n missing")
}

func (d deciphererStub) DecipherSignature(challenge string) (string, error) {
	if out, ok := d.sig[challenge]; ok {
		return out, nil
	}
	return "", errors.New("sig missing")
}

type providerStub struct {
	dec   Decipherer
	calls int
	err   error
}

func (p *providerStub) Load(context.Context, string) (Decipherer, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.dec, nil
}

func TestProviderBatchSolver_BulkSolveAndLookup(t *testing.T) {
	provider := &providerStub{
		dec: deciphererStub{
			n:   map[string]string{"abcd": "bcd"},
			sig: map[string]string{"xyz": "yz"},
		},
	}
	s := NewProviderBatchSolver(provider)
	s.AddN("abcd")
	s.AddSig("xyz")
	s.AddN("abcd") // dedupe

	if err := s.Solve(context.Background(), "/s/player/test/base.js"); err != nil {
		t.Fatalf("Solve() error = %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	if got, ok := s.N("abcd"); !ok || got != "bcd" {
		t.Fatalf("N(abcd) = %q, %v", got, ok)
	}
	if got, ok := s.Sig("xyz"); !ok || got != "yz" {
		t.Fatalf("Sig(xyz) = %q, %v", got, ok)
	}
}

func TestProviderBatchSolver_ProviderFailure(t *testing.T) {
	provider := &providerStub{err: errors.New("load failed")}
	s := NewProviderBatchSolver(provider)
	s.AddN("abcd")

	if err := s.Solve(context.Background(), "/s/player/test/base.js"); err == nil {
		t.Fatalf("expected error")
	}
	if _, ok := s.N("abcd"); ok {
		t.Fatalf("expected unresolved n challenge")
	}
}

func TestFallbackProviderBatchSolver_UsesSecondProvider(t *testing.T) {
	first := &providerStub{err: errors.New("first failed")}
	second := &providerStub{
		dec: deciphererStub{
			n:   map[string]string{"abcd": "bcd"},
			sig: map[string]string{"xyz": "yz"},
		},
	}
	s := NewFallbackProviderBatchSolver(first, second)
	s.AddN("abcd")
	s.AddSig("xyz")

	if err := s.Solve(context.Background(), "/s/player/test/base.js"); err != nil {
		t.Fatalf("Solve() error = %v", err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("provider calls first=%d second=%d, want 1/1", first.calls, second.calls)
	}
	if got, ok := s.N("abcd"); !ok || got != "bcd" {
		t.Fatalf("N(abcd) = %q, %v", got, ok)
	}
	if got, ok := s.Sig("xyz"); !ok || got != "yz" {
		t.Fatalf("Sig(xyz) = %q, %v", got, ok)
	}
}
