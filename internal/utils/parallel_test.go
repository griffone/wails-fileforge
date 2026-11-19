package utils

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestParallelForIndexRunsEveryIndexAndPreservesOrder(t *testing.T) {
	const count = 32
	results := make([]int, count)

	err := ParallelForIndex(context.Background(), count, 4, func(_ context.Context, index int) error {
		// Later indices finish first to prove the caller can rely on index order.
		time.Sleep(time.Duration(count-index) * time.Microsecond)
		results[index] = index * 2
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for index := 0; index < count; index++ {
		if results[index] != index*2 {
			t.Fatalf("index %d: got %d want %d", index, results[index], index*2)
		}
	}
}

func TestParallelForIndexBoundsConcurrency(t *testing.T) {
	const (
		count   = 24
		workers = 3
	)

	var running atomic.Int64
	var peak atomic.Int64
	release := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- ParallelForIndex(context.Background(), count, workers, func(_ context.Context, _ int) error {
			current := running.Add(1)
			for {
				observed := peak.Load()
				if current <= observed || peak.CompareAndSwap(observed, current) {
					break
				}
			}
			<-release
			running.Add(-1)
			return nil
		})
	}()

	// Give every worker time to pick up one item.
	deadline := time.Now().Add(2 * time.Second)
	for running.Load() < workers && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := peak.Load(); got > workers {
		t.Fatalf("concurrency exceeded the bound: got %d want <= %d", got, workers)
	}
	if got := peak.Load(); got < 1 {
		t.Fatalf("expected at least one worker, got %d", got)
	}
}

func TestParallelForIndexStopsLaunchingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var started atomic.Int64

	err := ParallelForIndex(ctx, 100, 2, func(_ context.Context, _ int) error {
		if started.Add(1) == 2 {
			cancel()
		}
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if got := started.Load(); got >= 100 {
		t.Fatalf("expected cancellation to stop new work, started %d items", got)
	}
}

func TestParallelForIndexReturnsFirstError(t *testing.T) {
	wantErr := errors.New("boom")

	err := ParallelForIndex(context.Background(), 10, 2, func(_ context.Context, index int) error {
		if index == 4 {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestParallelWorkerCountIsBounded(t *testing.T) {
	if got := ParallelWorkerCount(); got < 1 || got > maxParallelWorkers {
		t.Fatalf("unexpected worker count: %d", got)
	}
}
