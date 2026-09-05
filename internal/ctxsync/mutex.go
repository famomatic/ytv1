// Package ctxsync provides cancellation-aware synchronization for network work.
package ctxsync

import (
	"context"
	"sync"
)

// Mutex is a zero-value usable mutex with cancellable acquisition.
type Mutex struct {
	once sync.Once
	gate chan struct{}
}

func (m *Mutex) LockContext(ctx context.Context) error {
	m.once.Do(func() { m.gate = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-m.gate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Mutex) Lock()   { _ = m.LockContext(context.Background()) }
func (m *Mutex) Unlock() { <-m.gate }
