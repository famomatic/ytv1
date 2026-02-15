package stream

import (
	"context"

	"github.com/famomatic/ytv1/internal/challenge"
)

type SourceFormat struct {
	Itag            int
	URL             string
	SignatureCipher string
	ManifestURL     string
}

type Processor interface {
	CollectChallenges(formats []SourceFormat)
	Resolve(ctx context.Context, formats []SourceFormat, solver challenge.BatchSolver) ([]SourceFormat, error)
}
