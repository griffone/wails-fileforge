package preview

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrWorkerPoolStopped is returned when a job is enqueued after the pool has
// stopped.
var ErrWorkerPoolStopped = errors.New("preview: worker pool stopped")

// WorkerPool executes preview jobs using a handler provided at Start. Start and
// Stop are idempotent, and Stop may be called more than once.
type WorkerPool struct {
	queue chan *previewJob
	size  int
	wg    sync.WaitGroup

	mu         sync.Mutex
	stop       chan struct{}
	stopDone   chan struct{}
	started    bool
	stopped    bool
	tokens     chan struct{}
	refillStop chan struct{}
	refillDone chan struct{}
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
		queue:    make(chan *previewJob, maxQueue),
		size:     size,
		stop:     make(chan struct{}),
		stopDone: make(chan struct{}),
	}
}

// SetRateLimit enables a simple token-bucket rate limiter for job processing.
// rate: tokens per second. burst: maximum tokens stored. If rate <= 0, the
// limiter is disabled. SetRateLimit is safe to call concurrently with workers.
func (wp *WorkerPool) SetRateLimit(rate int, burst int) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.stopLimiterLocked()
	if rate <= 0 || wp.stopped {
		return
	}
	if burst <= 0 {
		burst = wp.size
	}
	interval := time.Second / time.Duration(rate)
	if interval <= 0 {
		interval = time.Nanosecond
	}

	tokens := make(chan struct{}, burst)
	for i := 0; i < burst; i++ {
		tokens <- struct{}{}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	wp.tokens = tokens
	wp.refillStop = stop
	wp.refillDone = done

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				select {
				case tokens <- struct{}{}:
				default:
					// burst full
				}
			case <-stop:
				return
			}
		}
	}()
}

// stopLimiterLocked stops the refill loop and releases blocked workers. The
// caller must hold wp.mu. The refill loop never takes wp.mu, so waiting for it
// while holding the lock cannot deadlock.
func (wp *WorkerPool) stopLimiterLocked() {
	if wp.refillStop != nil {
		close(wp.refillStop)
		<-wp.refillDone
		wp.refillStop = nil
		wp.refillDone = nil
	}
	if wp.tokens != nil {
		close(wp.tokens)
		wp.tokens = nil
	}
}

// Enqueue attempts to queue a job; returns an error if the queue is full or the
// pool has stopped.
func (wp *WorkerPool) Enqueue(job *previewJob) error {
	wp.mu.Lock()
	stopped := wp.stopped
	wp.mu.Unlock()
	if stopped {
		return ErrWorkerPoolStopped
	}

	select {
	case <-wp.stop:
		return ErrWorkerPoolStopped
	case wp.queue <- job:
		return nil
	default:
		return fmt.Errorf("preview: queue full")
	}
}

// Start runs worker goroutines that call handler(job) for each dequeued job.
// Calling Start more than once, or after Stop, is a no-op.
func (wp *WorkerPool) Start(handler func(*previewJob)) {
	if handler == nil {
		handler = func(*previewJob) {}
	}

	wp.mu.Lock()
	if wp.started || wp.stopped {
		wp.mu.Unlock()
		return
	}
	wp.started = true
	for i := 0; i < wp.size; i++ {
		wp.wg.Add(1)
	}
	wp.mu.Unlock()

	for i := 0; i < wp.size; i++ {
		go wp.worker(handler)
	}
}

func (wp *WorkerPool) worker(handler func(*previewJob)) {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.stop:
			return
		default:
		}

		select {
		case <-wp.stop:
			return
		case job := <-wp.queue:
			if job == nil {
				continue
			}
			if !wp.acquireToken() {
				return
			}
			handler(job)
		}
	}
}

// acquireToken waits for a rate-limit token. It returns false when the pool is
// stopping.
func (wp *WorkerPool) acquireToken() bool {
	wp.mu.Lock()
	tokens := wp.tokens
	wp.mu.Unlock()
	if tokens == nil {
		return true
	}

	select {
	case <-tokens:
		return true
	case <-wp.stop:
		return false
	}
}

// Stop signals workers to finish and waits for them. It is safe to call Stop
// multiple times, including before Start.
func (wp *WorkerPool) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	wp.mu.Lock()
	if !wp.stopped {
		wp.stopped = true
		close(wp.stop)
		wp.stopLimiterLocked()
		go func() {
			wp.wg.Wait()
			close(wp.stopDone)
		}()
	}
	wp.mu.Unlock()

	select {
	case <-wp.stopDone:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("preview: workerpool stop timeout: %w", ctx.Err())
	}
}
