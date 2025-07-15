package utils

import (
	"context"
	"sync"
)

const (
	DefaultWorkerCount = 4 // Default 4 threads
	DefaultBufferSize  = 8 // workerCount * 2
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
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workerCount int) *WorkerPool {
	if workerCount <= 0 {
		workerCount = DefaultWorkerCount
	}

	bufferSize := workerCount * 2
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		workerCount: workerCount,
		jobs:        make(chan Job, bufferSize), // Buffer to avoid blocking
		results:     make(chan Result, bufferSize),
		ctx:         ctx,
		cancel:      cancel,
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

			err := processFunc(job)
			wp.results <- Result{Job: job, Error: err}

		case <-wp.ctx.Done():
			return // Cancellation
		}
	}
}

// Submit a job to the pool
func (wp *WorkerPool) Submit(job Job) bool {
	select {
	case wp.jobs <- job:
		return true
	case <-wp.ctx.Done():
		return false
	}
}

// Close closes the worker pool and waits for all workers to finish.
func (wp *WorkerPool) Close() {
	// Close jobs channel if not already closed
	select {
	case <-wp.jobs:
		// Channel is already closed or drained
	default:
		close(wp.jobs)
	}

	wp.wg.Wait()
	close(wp.results)
}

func (wp *WorkerPool) CloseJobs() {
	select {
	case <-wp.jobs:
		// Channel is already closed
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
