package challenge

import "context"

type BatchSolver interface {
	AddSig(challenge string)
	AddN(challenge string)
	Solve(ctx context.Context, playerURL string) error
	Sig(challenge string) (string, bool)
	N(challenge string) (string, bool)
}
