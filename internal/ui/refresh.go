package ui

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/hcie123/guard9s/internal/model"
)

// One worker, one pending notification. Generation advances at request time,
// so even an already queued result cannot overwrite newer requested evidence.
type refreshState struct {
	latest atomic.Uint64
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (u *UI) requestRefresh() {
	u.refreshState.mu.Lock()
	u.refreshState.latest.Add(1)
	if u.refreshState.cancel != nil {
		u.refreshState.cancel()
	}
	u.refreshState.mu.Unlock()
	select {
	case u.refresh <- struct{}{}:
	default:
	}
}
func (u *UI) refreshWorker(ctx context.Context, source Source, previous model.Snapshot, updates chan result, wake func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.refresh:
		}
		if source == nil {
			continue
		}
		u.refreshState.mu.Lock()
		generation := u.refreshState.latest.Load()
		workCtx, cancel := context.WithCancel(ctx)
		u.refreshState.cancel = cancel
		u.refreshState.mu.Unlock()
		r := collectResult(workCtx, source, previous)
		u.refreshState.mu.Lock()
		u.refreshState.cancel = nil
		current := generation == u.refreshState.latest.Load() && workCtx.Err() == nil
		u.refreshState.mu.Unlock()
		cancel()
		if !current {
			continue
		}
		previous = r.snapshot
		r.generation = generation
		// Replace a pending older result; the worker never waits for redraw.
		select {
		case <-updates:
		default:
		}
		select {
		case updates <- r:
		case <-ctx.Done():
			return
		}
		wake()
	}
}
