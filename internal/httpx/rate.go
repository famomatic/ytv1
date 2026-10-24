package httpx

import (
	"context"
	"io"
	"sync"
	"time"
)

type rateKey struct{}
type rateBudget struct {
	mu   sync.Mutex
	next time.Time
	rate int64
}

// WithRateLimit shares one byte budget across all requests and workers in an
// operation. Nested downloaders retain the parent's budget.
func WithRateLimit(ctx context.Context, rate int64) context.Context {
	if rate <= 0 || ctx.Value(rateKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, rateKey{}, &rateBudget{rate: rate})
}

func RateReader(ctx context.Context, src io.Reader) io.Reader {
	b, _ := ctx.Value(rateKey{}).(*rateBudget)
	if b == nil {
		return src
	}
	return &budgetReader{ctx: ctx, src: src, budget: b}
}

type budgetReader struct {
	ctx    context.Context
	src    io.Reader
	budget *rateBudget
}

func (r *budgetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	// Bound each reservation so cancellation does not leave a huge debt.
	if cap := min(int64(32*1024), max(int64(1), r.budget.rate/10)); int64(len(p)) > cap {
		p = p[:cap]
	}
	n, err := r.src.Read(p)
	if n == 0 {
		return n, err
	}
	b := r.budget
	b.mu.Lock()
	now := time.Now()
	if b.next.Before(now) {
		b.next = now
	}
	b.next = b.next.Add(time.Duration(float64(n) / float64(b.rate) * float64(time.Second)))
	delay := time.Until(b.next)
	b.mu.Unlock()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case <-timer.C:
		return n, err
	}
}
