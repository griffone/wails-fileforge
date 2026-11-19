package utils

import (
	"context"
	"runtime"
	"sync"
)

const (
	// maxParallelWorkers keeps independent file work from starving the UI and
	// the external engines that already run their own thread pools.
	maxParallelWorkers = 8
)

// ParallelWorkerCount returns a bounded worker count for independent file
// operations. It scales with the machine but never exceeds maxParallelWorkers.
func ParallelWorkerCount() int {
	workers := runtime.NumCPU()
	if workers > maxParallelWorkers {
		workers = maxParallelWorkers
	}
	if workers < 1 {
		workers = 1
	}

	return workers
}

// ParallelForIndex runs fn for every index in [0, count) with bounded
// concurrency. Callers write results by index, so input order is preserved even
// though work completes out of order.
//
// The first non-nil error is returned after every started worker finishes.
// Cancellation stops new work from starting; in-flight work is expected to
// observe ctx itself (for example, exec.CommandContext kills the child).
func ParallelForIndex(ctx context.Context, count, workers int, fn func(context.Context, int) error) error {
	if count <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if workers <= 0 {
		workers = ParallelWorkerCount()
	}
	if workers > count {
		workers = count
	}

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	launch := func(index int) bool {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return false
		}

		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			if err := fn(ctx, i); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(index)

		return true
	}

	for index := 0; index < count; index++ {
		if !launch(index) {
			break
		}
	}

	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	return ctx.Err()
}
