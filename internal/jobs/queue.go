package jobs

import (
	"errors"
	"sort"
)

// ErrQueueFull is returned by Submit when the pending queue is saturated.
var ErrQueueFull = errors.New("jobs: pending queue is full")

// ErrEngineClosed is returned by Submit after Shutdown started.
var ErrEngineClosed = errors.New("jobs: engine is closed")

// dispatchLoop is the only goroutine that moves queued jobs into execution. It
// wakes on new submissions and on capacity releases, starts every runnable job,
// then sleeps until the next signal.
func (e *Engine) dispatchLoop() {
	defer close(e.dispatcherDone)

	for {
		select {
		case <-e.stopCh:
			return
		case <-e.wake:
		}

		for {
			select {
			case <-e.stopCh:
				return
			default:
			}

			state := e.popRunnable()
			if state == nil {
				break
			}

			e.wg.Add(1)
			go e.runJob(state)
		}
	}
}

// enqueue inserts a job in priority order: interactive first, then automation,
// then preview, with FIFO order inside a priority. It does not signal the
// dispatcher; the caller persists the initial snapshot first.
func (e *Engine) enqueue(state *jobState) error {
	e.queueMu.Lock()
	defer e.queueMu.Unlock()

	if e.closed.Load() {
		return ErrEngineClosed
	}
	if len(e.queue) >= e.opts.MaxQueue {
		return ErrQueueFull
	}

	index := sort.Search(len(e.queue), func(i int) bool {
		queued := e.queue[i]
		if queued.priority != state.priority {
			return queued.priority > state.priority
		}
		return queued.seq > state.seq
	})

	e.queue = append(e.queue, nil)
	copy(e.queue[index+1:], e.queue[index:])
	e.queue[index] = state
	state.queued = true

	return nil
}

// signal wakes the dispatcher without blocking. A pending wake is enough to
// make the loop rescan the queue.
func (e *Engine) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// popRunnable removes and returns the first queued job whose class and the
// global total have capacity. Scanning past a blocked high-priority job is what
// avoids head-of-line blocking between classes.
func (e *Engine) popRunnable() *jobState {
	e.queueMu.Lock()
	defer e.queueMu.Unlock()

	if e.closed.Load() || e.totalRunning >= e.opts.Limits.Total {
		return nil
	}

	for index, state := range e.queue {
		if e.running[state.class] >= e.opts.Limits.capacity(state.class) {
			continue
		}

		e.queue = append(e.queue[:index], e.queue[index+1:]...)
		state.queued = false
		e.totalRunning++
		e.running[state.class]++

		return state
	}

	return nil
}

// release frees one running slot and wakes the dispatcher to fill it.
func (e *Engine) release(class Class) {
	e.queueMu.Lock()
	if e.totalRunning > 0 {
		e.totalRunning--
	}
	if e.running[class] > 0 {
		e.running[class]--
	}
	e.queueMu.Unlock()

	e.signal()
}

// drainQueue removes every queued job. The caller completes them as cancelled.
func (e *Engine) drainQueue() []*jobState {
	e.queueMu.Lock()
	defer e.queueMu.Unlock()

	queued := e.queue
	e.queue = nil
	for _, state := range queued {
		state.queued = false
	}

	return queued
}

// removeQueued removes one job from the pending queue. It reports whether the
// job was still queued.
func (e *Engine) removeQueued(state *jobState) bool {
	e.queueMu.Lock()
	defer e.queueMu.Unlock()

	if !state.queued {
		return false
	}
	for index, queued := range e.queue {
		if queued != state {
			continue
		}
		e.queue = append(e.queue[:index], e.queue[index+1:]...)
		state.queued = false

		return true
	}
	state.queued = false

	return false
}
