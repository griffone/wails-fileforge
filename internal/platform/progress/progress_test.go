package progress

import (
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu      sync.Mutex
	current time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{current: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.current
}

func (c *fakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(delta)
}

func TestReportCoalescesWithinInterval(t *testing.T) {
	clock := newFakeClock()
	var emitted []int
	reporter := NewWithClock(100*time.Millisecond, func(v int) {
		emitted = append(emitted, v)
	}, clock.Now)

	reporter.Report(1) // first value always emits
	clock.Advance(50 * time.Millisecond)
	reporter.Report(2)
	clock.Advance(40 * time.Millisecond)
	reporter.Report(3)
	if len(emitted) != 1 || emitted[0] != 1 {
		t.Fatalf("emitted = %v, want [1] while inside the interval", emitted)
	}

	clock.Advance(10 * time.Millisecond) // exactly one interval after the first emit
	reporter.Report(4)
	if len(emitted) != 2 || emitted[1] != 4 {
		t.Fatalf("emitted = %v, want [1 4] at the interval boundary", emitted)
	}

	reporter.Flush()
	if len(emitted) != 2 {
		t.Fatalf("Flush emitted an extra value: %v", emitted)
	}
}

func TestFlushEmitsPendingValue(t *testing.T) {
	clock := newFakeClock()
	var emitted []int
	reporter := NewWithClock(100*time.Millisecond, func(v int) {
		emitted = append(emitted, v)
	}, clock.Now)

	reporter.Report(1)
	clock.Advance(20 * time.Millisecond)
	reporter.Report(2)
	reporter.Flush()

	if len(emitted) != 2 || emitted[0] != 1 || emitted[1] != 2 {
		t.Fatalf("emitted = %v, want [1 2]", emitted)
	}

	reporter.Flush()
	if len(emitted) != 2 {
		t.Fatalf("second Flush emitted an extra value: %v", emitted)
	}
}

func TestEmitNowAlwaysEmitsAndResetsInterval(t *testing.T) {
	clock := newFakeClock()
	var emitted []int
	reporter := NewWithClock(100*time.Millisecond, func(v int) {
		emitted = append(emitted, v)
	}, clock.Now)

	reporter.Report(1)
	clock.Advance(50 * time.Millisecond)
	reporter.EmitNow(2)
	if len(emitted) != 2 || emitted[1] != 2 {
		t.Fatalf("emitted = %v, want [1 2] after EmitNow", emitted)
	}

	clock.Advance(50 * time.Millisecond) // only 50ms since EmitNow
	reporter.Report(3)
	if len(emitted) != 2 {
		t.Fatalf("emitted = %v, want no emission 50ms after EmitNow", emitted)
	}

	clock.Advance(50 * time.Millisecond) // 100ms since EmitNow
	reporter.Report(4)
	if len(emitted) != 3 || emitted[2] != 4 {
		t.Fatalf("emitted = %v, want [1 2 4]", emitted)
	}
}

func TestReportUsesDefaultInterval(t *testing.T) {
	clock := newFakeClock()
	var emitted []int
	reporter := NewWithClock(0, func(v int) {
		emitted = append(emitted, v)
	}, clock.Now)

	reporter.Report(1)
	clock.Advance(DefaultInterval - time.Millisecond)
	reporter.Report(2)
	if len(emitted) != 1 {
		t.Fatalf("emitted = %v, want a single value before the default interval", emitted)
	}

	clock.Advance(time.Millisecond)
	reporter.Report(3)
	if len(emitted) != 2 || emitted[1] != 3 {
		t.Fatalf("emitted = %v, want [1 3] at the default interval", emitted)
	}
}

func TestConcurrentReports(t *testing.T) {
	var mutex sync.Mutex
	emitted := 0
	reporter := New[int](time.Hour, func(int) {
		mutex.Lock()
		emitted++
		mutex.Unlock()
	})

	var waitGroup sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			for index := 0; index < 100; index++ {
				reporter.Report(worker*1000 + index)
			}
		}(worker)
	}
	waitGroup.Wait()
	reporter.Flush()

	mutex.Lock()
	defer mutex.Unlock()
	if emitted != 2 {
		t.Fatalf("emitted = %d, want exactly the first report plus one flush", emitted)
	}
}

func TestNilReporterAndNilEmitAreSafe(t *testing.T) {
	var nilReporter *Reporter[int]
	nilReporter.Report(1)
	nilReporter.EmitNow(1)
	nilReporter.Flush()

	reporter := New[int](time.Second, nil)
	reporter.Report(1)
	reporter.EmitNow(2)
	reporter.Flush()
}

func TestEmitNowResetsPendingValue(t *testing.T) {
	clock := newFakeClock()
	var emitted []int
	reporter := NewWithClock(100*time.Millisecond, func(v int) {
		emitted = append(emitted, v)
	}, clock.Now)

	reporter.Report(1)
	reporter.Report(2)
	reporter.EmitNow(3)
	reporter.Flush()

	if len(emitted) != 2 || emitted[0] != 1 || emitted[1] != 3 {
		t.Fatalf("emitted = %v, want [1 3]; the superseded pending value must not flush", emitted)
	}
}
