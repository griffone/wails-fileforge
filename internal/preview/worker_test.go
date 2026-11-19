package preview

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitForCount(t *testing.T, counter *atomic.Int32, want int32) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("expected %d processed jobs, got %d", want, counter.Load())
}

func TestWorkerPoolStopIsIdempotent(t *testing.T) {
	wp := NewWorkerPool(2, 4)
	wp.Start(func(*previewJob) {})

	ctx := context.Background()
	if err := wp.Stop(ctx); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := wp.Stop(ctx); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}

func TestWorkerPoolStopBeforeStartIsIdempotent(t *testing.T) {
	wp := NewWorkerPool(2, 4)
	ctx := context.Background()

	if err := wp.Stop(ctx); err != nil {
		t.Fatalf("first stop: %v", err)
	}
	if err := wp.Stop(ctx); err != nil {
		t.Fatalf("second stop: %v", err)
	}

	// Start after Stop must not resurrect workers.
	wp.Start(func(*previewJob) {})
}

func TestWorkerPoolStopWithEmptyQueue(t *testing.T) {
	wp := NewWorkerPool(4, 8)
	wp.Start(func(*previewJob) {})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := wp.Stop(ctx); err != nil {
		t.Fatalf("stop with an empty queue: %v", err)
	}
}

func TestWorkerPoolEnqueueAfterStopReturnsClearError(t *testing.T) {
	wp := NewWorkerPool(1, 1)
	wp.Start(func(*previewJob) {})
	if err := wp.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	err := wp.Enqueue(&previewJob{})
	if !errors.Is(err, ErrWorkerPoolStopped) {
		t.Fatalf("expected ErrWorkerPoolStopped, got %v", err)
	}
}

func TestWorkerPoolProcessesJobsWithRateLimit(t *testing.T) {
	wp := NewWorkerPool(2, 8)
	wp.SetRateLimit(1000, 2)

	var processed atomic.Int32
	wp.Start(func(*previewJob) {
		processed.Add(1)
	})
	t.Cleanup(func() {
		_ = wp.Stop(context.Background())
	})

	for i := 0; i < 4; i++ {
		if err := wp.Enqueue(&previewJob{}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	waitForCount(t, &processed, 4)
}

func TestWorkerPoolSetRateLimitIsSafeDuringProcessing(t *testing.T) {
	wp := NewWorkerPool(2, 8)
	wp.Start(func(*previewJob) {})
	t.Cleanup(func() {
		_ = wp.Stop(context.Background())
	})

	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for iteration := 0; iteration < 25; iteration++ {
				if iteration%5 == 0 {
					wp.SetRateLimit(0, 0)
					continue
				}
				wp.SetRateLimit((worker+1)*100, iteration%3+1)
			}
		}(worker)
	}
	wg.Wait()

	wp.SetRateLimit(0, 0)
}
