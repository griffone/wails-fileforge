package preview

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// WorkerPool executes preview jobs using a handler provided at Start.
type WorkerPool struct {
	queue chan *previewJob
	size  int
	wg    sync.WaitGroup
	stop  chan struct{}
	// simple token bucket limiter (nil == disabled)
	tokens     chan struct{}
	refillStop chan struct{}
}

// NewWorkerPool constructs a worker pool with the given size and maxQueue capacity.
func NewWorkerPool(size int, maxQueue int) *WorkerPool {
	if size <= 0 {
		size = 1
	}
	if maxQueue <= 0 {
		maxQueue = 1024
	}
	return &WorkerPool{
		queue: make(chan *previewJob, maxQueue),
		size:  size,
		stop:  make(chan struct{}),
	}
}

// SetRateLimit enables a simple token-bucket rate limiter for job processing.
// rate: tokens per second. burst: maximum tokens stored. If rate <= 0, limiter disabled.
func (wp *WorkerPool) SetRateLimit(rate int, burst int) {
	// stop existing refill loop if present
	if wp.refillStop != nil {
		close(wp.refillStop)
		wp.refillStop = nil
	}
	if rate <= 0 {
		wp.tokens = nil
		return
	}
	if burst <= 0 {
		burst = wp.size
	}
	wp.tokens = make(chan struct{}, burst)
	// fill initial burst
	for i := 0; i < burst; i++ {
		wp.tokens <- struct{}{}
	}
	stop := make(chan struct{})
	wp.refillStop = stop
	go func() {
		ticker := time.NewTicker(time.Second / time.Duration(rate))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case wp.tokens <- struct{}{}:
				default:
					// burst full
				}
			case <-stop:
				return
			}
		}
	}()
}

// Enqueue attempts to queue a job; returns error if queue is full.
func (wp *WorkerPool) Enqueue(job *previewJob) error {
	select {
	case wp.queue <- job:
		return nil
	default:
		return fmt.Errorf("preview: queue full")
	}
}

// Start runs worker goroutines that call handler(job) for each dequeued job.
func (wp *WorkerPool) Start(handler func(*previewJob)) {
	for i := 0; i < wp.size; i++ {
		wp.wg.Add(1)
		go func() {
			defer wp.wg.Done()
			for {
				select {
				case job := <-wp.queue:
					// if tokens channel is configured, acquire token before processing
					if wp.tokens != nil {
						select {
						case <-wp.tokens:
							// got token
						case <-wp.stop:
							return
						}
					}
					handler(job)
				case <-wp.stop:
					return
				}
			}
		}()
	}
}

// Stop signals workers to finish and waits for them.
func (wp *WorkerPool) Stop(ctx context.Context) error {
	close(wp.stop)
	done := make(chan struct{})
	go func() {
		wp.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("preview: workerpool stop timeout: %w", ctx.Err())
	}
}
