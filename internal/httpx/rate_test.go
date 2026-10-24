package httpx

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateBudgetSharedByParallelReaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx = WithRateLimit(ctx, 80)
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := io.ReadAll(RateReader(WithRateLimit(ctx, 80), strings.NewReader(strings.Repeat("x", 16))))
			if err != nil || len(data) != 16 {
				t.Errorf("read: %d %v", len(data), err)
			}
		}()
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed < 350*time.Millisecond {
		t.Fatalf("readers received separate budgets: %v", elapsed)
	}
}
