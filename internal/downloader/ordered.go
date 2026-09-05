package downloader

import (
	"context"
	"errors"
	"sync"
)

// orderedFetch uses O(window) workers and slots regardless of playlist length.
// A slot is reused only after its body has been written, bounding fetch-ahead.
func orderedFetch(ctx context.Context, n, window int, fetch func(context.Context, int) ([]byte, error), write func(context.Context, int, []byte) error) error {
	if n == 0 {
		return nil
	}
	window = min(max(1, window), n)
	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	type result struct {
		body []byte
		err  error
	}
	slots := make([]chan result, window)
	jobs := make(chan int, window)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var root error
	for i := range slots {
		slots[i] = make(chan result, 1)
		jobs <- i
	}
	for k := 0; k < window; k++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case i := <-jobs:
					body, err := fetch(ctx, i)
					if err != nil && !errors.Is(err, context.Canceled) {
						mu.Lock()
						if root == nil {
							root = err
						}
						mu.Unlock()
					}
					select {
					case slots[i%window] <- result{body, err}:
					case <-ctx.Done():
						return
					}
					if err != nil {
						cancel()
						return
					}
				}
			}
		}()
	}
	var runErr error
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			runErr = ctx.Err()
		case r := <-slots[i%window]:
			runErr = r.err
			if runErr == nil {
				runErr = write(ctx, i, r.body)
			}
		}
		if runErr != nil {
			break
		}
		if i+window < n {
			jobs <- i + window
		}
	}
	cancel()
	wg.Wait()
	if parent.Err() != nil {
		return parent.Err()
	}
	if runErr != nil {
		mu.Lock()
		defer mu.Unlock()
		if root != nil {
			return root
		}
	}
	return runErr
}

func fragmentWindow(cfg TransportConfig) int {
	budget := cfg.MaxBufferedBytes
	if budget <= 0 {
		budget = 512 << 20
	}
	size := cfg.MaxSegmentBytes
	if size <= 0 {
		size = 100 << 20
	}
	return min(max(1, cfg.MaxConcurrency), int(max(int64(1), budget/size)))
}
