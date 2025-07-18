package utils

import (
	"context"
	"sync"
)

const (
	DefaultWorkerCount = 4                      // Default 4 threads
	DefaultBufferSize  = DefaultWorkerCount * 2 // Smaller buffer since we use blocking submission
)

// Job represents a conversion task
type Job struct {
	InputFile  string
	OutputFile string
	Data       []byte
	Options    map[string]any
}

// Result represents the result of a conversion
type Result struct {
	Job   Job
	Error error
}

// WorkerPool manages a pool of workers for concurrent processing
type WorkerPool struct {
	workerCount int
	jobs        chan Job
	results     chan Result
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	closed      bool
	closeMutex  sync.RWMutex
}

// NewWorkerPool creates a new worker pool with a parent context
func NewWorkerPool(ctx context.Context, workerCount int) *WorkerPool {
	if workerCount <= 0 {
		workerCount = DefaultWorkerCount
	}

	bufferSize := DefaultBufferSize
	if bufferSize < 8 {
		bufferSize = 8
	}

	// Create a child context that can be cancelled independently
	childCtx, cancel := context.WithCancel(ctx)
	return &WorkerPool{
		workerCount: workerCount,
		jobs:        make(chan Job, bufferSize),
		results:     make(chan Result, bufferSize),
		ctx:         childCtx,
		cancel:      cancel,
		closed:      false,
	}
}

// Start the workers
func (wp *WorkerPool) Start(processFunc func(Job) error) {
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go wp.worker(processFunc)
	}
}

// worker processes channel jobs
func (wp *WorkerPool) worker(processFunc func(Job) error) {
	defer wp.wg.Done()

	for {
		select {
		case job, ok := <-wp.jobs:
			if !ok {
				return // Channel closed
			}

			// Check context before processing
			select {
			case <-wp.ctx.Done():
				return // Context cancelled
			default:
			}

			err := processFunc(job)

			// Try to send result, but don't block if context is cancelled
			select {
			case wp.results <- Result{Job: job, Error: err}:
			case <-wp.ctx.Done():
				return // Context cancelled while sending result
			}

		case <-wp.ctx.Done():
			return // Cancellation
		}
	}
}

// Submit a job to the pool (blocking - waits for space)
func (wp *WorkerPool) Submit(job Job) bool {
	wp.closeMutex.RLock()
	defer wp.closeMutex.RUnlock()

	if wp.closed {
		return false
	}

	// This will block until there's space in the channel or context is cancelled
	select {
	case wp.jobs <- job:
		return true
	case <-wp.ctx.Done():
		return false
	}
}

// Close closes the worker pool and waits for all workers to finish.
func (wp *WorkerPool) Close() {
	wp.closeMutex.Lock()
	if wp.closed {
		wp.closeMutex.Unlock()
		return
	}
	wp.closed = true
	wp.closeMutex.Unlock()

	// Cancel context to stop workers
	wp.cancel()

	// Wait for all workers to finish
	wp.wg.Wait()

	// Close channels safely
	select {
	case <-wp.jobs:
	default:
		close(wp.jobs)
	}

	close(wp.results)
}

func (wp *WorkerPool) CloseJobs() {
	wp.closeMutex.RLock()
	defer wp.closeMutex.RUnlock()

	if wp.closed {
		return
	}

	// Close jobs channel if not already closed
	select {
	case <-wp.ctx.Done():
		// Context cancelled, jobs might already be closed
	default:
		close(wp.jobs)
	}
}

// Results returns the results channel
func (wp *WorkerPool) Results() <-chan Result {
	return wp.results
}

// Cancel all workers
func (wp *WorkerPool) Cancel() {
	wp.cancel()
}

// IsClosed returns whether the worker pool is closed
func (wp *WorkerPool) IsClosed() bool {
	wp.closeMutex.RLock()
	defer wp.closeMutex.RUnlock()
	return wp.closed
}
