// Package progress coalesces high-frequency progress updates so a slow
// consumer (UI event, disk snapshot) receives at most one emission per
// interval while always having access to the latest value on flush.
package progress

import (
	"sync"
	"time"
)

// DefaultInterval is the coalescing window used when an invalid interval is
// provided. It keeps progress at 10 Hz or less.
const DefaultInterval = 100 * time.Millisecond

// Reporter coalesces values of type T. It is safe for concurrent use. The emit
// callback runs while the reporter is locked, so it must not call back into the
// reporter and should return quickly.
type Reporter[T any] struct {
	mu         sync.Mutex
	interval   time.Duration
	emit       func(T)
	now        func() time.Time
	last       time.Time
	emitted    bool
	pending    T
	hasPending bool
}

// New returns a reporter that emits at most once per interval using the real
// clock.
func New[T any](interval time.Duration, emit func(T)) *Reporter[T] {
	return NewWithClock(interval, emit, time.Now)
}

// NewWithClock is New with an injectable clock for tests.
func NewWithClock[T any](interval time.Duration, emit func(T), now func() time.Time) *Reporter[T] {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if now == nil {
		now = time.Now
	}

	return &Reporter[T]{
		interval: interval,
		emit:     emit,
		now:      now,
	}
}

// Report emits v when the interval has elapsed since the last emission, and
// otherwise keeps v as the pending value for the next opportunity.
func (r *Reporter[T]) Report(v T) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.emit == nil {
		return
	}

	now := r.now()
	if r.emitted && now.Before(r.last.Add(r.interval)) {
		r.pending = v
		r.hasPending = true
		return
	}

	r.emitLocked(v, now)
}

// EmitNow always emits v and resets the interval.
func (r *Reporter[T]) EmitNow(v T) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.emit == nil {
		return
	}

	r.emitLocked(v, r.now())
}

// Flush emits the pending value, if any, and resets the interval.
func (r *Reporter[T]) Flush() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.emit == nil || !r.hasPending {
		return
	}

	r.emitLocked(r.pending, r.now())
}

func (r *Reporter[T]) emitLocked(v T, now time.Time) {
	r.emitted = true
	r.last = now
	r.hasPending = false
	var zero T
	r.pending = zero
	r.emit(v)
}
