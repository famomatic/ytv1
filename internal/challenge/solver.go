package challenge

import (
	"context"
	"strings"
)

type BatchSolver interface {
	AddSig(challenge string)
	AddN(challenge string)
	Solve(ctx context.Context, playerURL string) error
	Sig(challenge string) (string, bool)
	N(challenge string) (string, bool)
}

type Decipherer interface {
	DecipherN(challenge string) (string, error)
	DecipherSignature(challenge string) (string, error)
}

type DeciphererProvider interface {
	Load(ctx context.Context, playerURL string) (Decipherer, error)
}

type providerBatchSolver struct {
	provider DeciphererProvider
	pendingN map[string]struct{}
	pendingS map[string]struct{}
	n        map[string]string
	s        map[string]string
}

func NewProviderBatchSolver(provider DeciphererProvider) BatchSolver {
	return &providerBatchSolver{
		provider: provider,
		pendingN: make(map[string]struct{}),
		pendingS: make(map[string]struct{}),
		n:        make(map[string]string),
		s:        make(map[string]string),
	}
}

type providerChainBatchSolver struct {
	providers []DeciphererProvider
	pendingN  map[string]struct{}
	pendingS  map[string]struct{}
	n         map[string]string
	s         map[string]string
}

// NewFallbackProviderBatchSolver tries providers in order and keeps solved
// outputs across providers. A provider failure does not abort subsequent
// providers.
func NewFallbackProviderBatchSolver(providers ...DeciphererProvider) BatchSolver {
	filtered := make([]DeciphererProvider, 0, len(providers))
	for _, p := range providers {
		if p != nil {
			filtered = append(filtered, p)
		}
	}
	return &providerChainBatchSolver{
		providers: filtered,
		pendingN:  make(map[string]struct{}),
		pendingS:  make(map[string]struct{}),
		n:         make(map[string]string),
		s:         make(map[string]string),
	}
}

func (s *providerBatchSolver) AddSig(challenge string) {
	challenge = strings.TrimSpace(challenge)
	if challenge == "" {
		return
	}
	s.pendingS[challenge] = struct{}{}
}

func (s *providerBatchSolver) AddN(challenge string) {
	challenge = strings.TrimSpace(challenge)
	if challenge == "" {
		return
	}
	s.pendingN[challenge] = struct{}{}
}

func (s *providerBatchSolver) Solve(ctx context.Context, playerURL string) error {
	if s == nil || s.provider == nil {
		return nil
	}
	if len(s.pendingN) == 0 && len(s.pendingS) == 0 {
		return nil
	}
	decipherer, err := s.provider.Load(ctx, playerURL)
	if err != nil {
		return err
	}
	for challenge := range s.pendingN {
		if _, ok := s.n[challenge]; ok {
			continue
		}
		decoded, err := decipherer.DecipherN(challenge)
		if err != nil {
			continue
		}
		s.n[challenge] = decoded
	}
	for challenge := range s.pendingS {
		if _, ok := s.s[challenge]; ok {
			continue
		}
		decoded, err := decipherer.DecipherSignature(challenge)
		if err != nil {
			continue
		}
		s.s[challenge] = decoded
	}
	return nil
}

func (s *providerBatchSolver) Sig(challenge string) (string, bool) {
	if s == nil {
		return "", false
	}
	out, ok := s.s[challenge]
	return out, ok
}

func (s *providerBatchSolver) N(challenge string) (string, bool) {
	if s == nil {
		return "", false
	}
	out, ok := s.n[challenge]
	return out, ok
}

func (s *providerChainBatchSolver) AddSig(challenge string) {
	challenge = strings.TrimSpace(challenge)
	if challenge == "" {
		return
	}
	s.pendingS[challenge] = struct{}{}
}

func (s *providerChainBatchSolver) AddN(challenge string) {
	challenge = strings.TrimSpace(challenge)
	if challenge == "" {
		return
	}
	s.pendingN[challenge] = struct{}{}
}

func (s *providerChainBatchSolver) Solve(ctx context.Context, playerURL string) error {
	if s == nil || len(s.providers) == 0 {
		return nil
	}
	if len(s.pendingN) == 0 && len(s.pendingS) == 0 {
		return nil
	}

	remainingN := make(map[string]struct{}, len(s.pendingN))
	remainingS := make(map[string]struct{}, len(s.pendingS))
	for challenge := range s.pendingN {
		remainingN[challenge] = struct{}{}
	}
	for challenge := range s.pendingS {
		remainingS[challenge] = struct{}{}
	}

	var lastErr error
	for _, provider := range s.providers {
		if len(remainingN) == 0 && len(remainingS) == 0 {
			break
		}
		decipherer, err := provider.Load(ctx, playerURL)
		if err != nil {
			lastErr = err
			continue
		}
		for challenge := range remainingN {
			decoded, err := decipherer.DecipherN(challenge)
			if err != nil {
				continue
			}
			s.n[challenge] = decoded
			delete(remainingN, challenge)
		}
		for challenge := range remainingS {
			decoded, err := decipherer.DecipherSignature(challenge)
			if err != nil {
				continue
			}
			s.s[challenge] = decoded
			delete(remainingS, challenge)
		}
	}
	if len(remainingN) == len(s.pendingN) && len(remainingS) == len(s.pendingS) && lastErr != nil {
		return lastErr
	}
	return nil
}

func (s *providerChainBatchSolver) Sig(challenge string) (string, bool) {
	if s == nil {
		return "", false
	}
	out, ok := s.s[challenge]
	return out, ok
}

func (s *providerChainBatchSolver) N(challenge string) (string, bool) {
	if s == nil {
		return "", false
	}
	out, ok := s.n[challenge]
	return out, ok
}
