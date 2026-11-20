package automation

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

// TestWatcherRunTimeoutCancelsRecipeJob proves a timed-out automation run stops
// the recipe job instead of leaving it running in the background.
func TestWatcherRunTimeoutCancelsRecipeJob(t *testing.T) {
	runner := newFakeRecipeRunner()
	runner.pollsBeforeTerminal = 100000
	service, store := newTestService(t, runner, nil)
	service.runTimeout = 30 * time.Millisecond

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRule{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	writeTestFile(t, filepath.Join(watch, "slow.txt"), "slow run")

	waitFor(t, 3*time.Second, "the timed-out run to be recorded", func() bool {
		stored, found := store.Get(rule.ID)

		return found && stored.LastStatus == models.JobStatusFailed
	})

	if runner.cancelCount() != 1 {
		t.Fatalf("expected the timed-out run to cancel its recipe job, got %+v", runner.cancelledJobs())
	}

	stored, found := store.Get(rule.ID)
	if !found {
		t.Fatal("rule disappeared from the store")
	}
	if stored.LastStatus != models.JobStatusFailed {
		t.Fatalf("expected a failed last run, got %+v", stored)
	}
	if stored.LastMessage != "The recipe did not finish in time." {
		t.Fatalf("unexpected timeout message: %q", stored.LastMessage)
	}
	if cancelled := runner.cancelledJobs(); len(cancelled) != 1 || cancelled[0] == "" {
		t.Fatalf("unexpected cancelled recipe jobs: %+v", cancelled)
	}
}

// TestWatcherBurstProcessesFilesSequentially proves a burst of accepted files
// is queued behind one per-rule worker instead of one goroutine per file.
func TestWatcherBurstProcessesFilesSequentially(t *testing.T) {
	runner := newFakeRecipeRunner()
	runner.runDelay = 20 * time.Millisecond
	runner.pollsBeforeTerminal = 1
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	saveTestRule(t, service, models.AutomationRule{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	const fileCount = 6
	for index := 0; index < fileCount; index++ {
		writeTestFile(t, filepath.Join(watch, fmt.Sprintf("burst-%d.txt", index)), "burst")
	}

	waitForRunCount(t, runner, fileCount, 15*time.Second)
	assertRunStaysAt(t, runner, fileCount, 200*time.Millisecond)

	if max := runner.maxConcurrentRuns(); max != 1 {
		t.Fatalf("%d concurrent recipe runs, want exactly one per rule", max)
	}

	seen := make(map[string]struct{}, fileCount)
	for _, request := range runner.runsSnapshot() {
		if len(request.InputPaths) != 1 {
			t.Fatalf("unexpected input paths: %+v", request.InputPaths)
		}
		seen[request.InputPaths[0]] = struct{}{}
	}
	if len(seen) != fileCount {
		t.Fatalf("processed %d distinct files, want %d: %+v", len(seen), fileCount, seen)
	}
}

// TestWatcherQueueDropsWhenFull proves the per-rule queue bounds accepted work
// and drops new paths without blocking the fsnotify loop.
func TestWatcherQueueDropsWhenFull(t *testing.T) {
	service, _ := newTestService(t, newFakeRecipeRunner(), nil)
	rule := models.AutomationRule{
		ID:       "rule_queue",
		Name:     "Queue",
		Enabled:  true,
		WatchDir: t.TempDir(),
		RecipeID: testRecipeID,
	}

	watcher, err := newRuleWatcher(service, rule, context.Background())
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	t.Cleanup(func() {
		watcher.stop(time.Now().Add(time.Second))
	})

	if cap(watcher.queue) != watcherQueueCapacity {
		t.Fatalf("queue capacity = %d, want %d", cap(watcher.queue), watcherQueueCapacity)
	}
	for index := 0; index < cap(watcher.queue); index++ {
		if !watcher.enqueue(filepath.Join("/tmp", fmt.Sprintf("queued-%d.txt", index))) {
			t.Fatalf("enqueue %d was rejected before the queue was full", index)
		}
	}

	done := make(chan bool, 1)
	go func() {
		done <- watcher.enqueue(filepath.Join("/tmp", "overflow.txt"))
	}()

	select {
	case accepted := <-done:
		if accepted {
			t.Fatal("expected the full queue to drop the path")
		}
	case <-time.After(time.Second):
		t.Fatal("enqueue blocked while the queue was full")
	}

	if len(watcher.queue) != watcherQueueCapacity {
		t.Fatalf("queue length = %d after a drop, want %d", len(watcher.queue), watcherQueueCapacity)
	}
}

// stopAwareRunner blocks the first run until cancellation, waits a short
// unwinding window, then reports the exit. Later runs delegate to the fake.
type stopAwareRunner struct {
	*fakeRecipeRunner

	calls   atomic.Int32
	entered chan struct{}
	exited  chan struct{}
}

func (r *stopAwareRunner) Run(ctx context.Context, req models.RecipeRequest) models.RunRecipeResponse {
	if r.calls.Add(1) == 1 {
		select {
		case r.entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		time.Sleep(75 * time.Millisecond)
		select {
		case r.exited <- struct{}{}:
		default:
		}

		return models.RunRecipeResponse{
			Success: true,
			Message: "recipe submitted",
			JobID:   "job_stop_aware",
			Status:  models.JobStatusQueued,
		}
	}

	return r.fakeRecipeRunner.Run(ctx, req)
}

// TestStartRuleWaitsForDeadWatcherBeforeReplacement proves replacing a dead
// watcher stops the old generation before the new one starts, preventing
// duplicate processing.
func TestStartRuleWaitsForDeadWatcherBeforeReplacement(t *testing.T) {
	runner := &stopAwareRunner{
		fakeRecipeRunner: newFakeRecipeRunner(),
		entered:          make(chan struct{}, 1),
		exited:           make(chan struct{}, 1),
	}
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRule{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	service.mu.Lock()
	old := service.watchers[rule.ID]
	service.mu.Unlock()
	if old == nil {
		t.Fatal("expected a running watcher")
	}

	writeTestFile(t, filepath.Join(watch, "first.txt"), "first")
	select {
	case <-runner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first run to start")
	}

	old.markDead("replacement test")
	service.startRule(rule)

	select {
	case <-runner.exited:
	default:
		t.Fatal("startRule returned before the old watcher goroutine exited")
	}

	service.mu.Lock()
	replacement := service.watchers[rule.ID]
	service.mu.Unlock()
	if replacement == nil {
		t.Fatal("startRule did not install a replacement watcher")
	}
	if replacement == old {
		t.Fatal("the dead watcher was not replaced")
	}

	writeTestFile(t, filepath.Join(watch, "second.txt"), "second")
	waitForRunCount(t, runner.fakeRecipeRunner, 1, 5*time.Second)
}
