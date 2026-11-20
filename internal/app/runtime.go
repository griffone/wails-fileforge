package app

import (
	"context"
	"sync"
)

// Runtime holds the application lifetime context shared by the bound services.
// Wails cancels a bound-method context as soon as the RPC returns, so
// background work must parent on this context instead.
type Runtime struct {
	mu  sync.RWMutex
	ctx context.Context
}

// Set stores the application lifetime context.
func (r *Runtime) Set(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ctx = ctx
}

// ContextOrBackground returns the application lifetime context, falling back to
// a background context in headless tests and early calls.
func (r *Runtime) ContextOrBackground() context.Context {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.ctx == nil {
		return context.Background()
	}

	return r.ctx
}
