package ctxsync

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCanceledWaitDoesNotAcquire(t *testing.T) {
	var m Mutex
	m.Lock()
	defer m.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := m.LockContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
