package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestService builds a service without the default cache or janitors so a
// test controls worker scheduling directly.
func newTestService(t *testing.T, cfg Config) *PreviewService {
	t.Helper()

	cfg = applyPreviewDefaults(cfg)
	runner := NewLocalExecRunner()
	svc := &PreviewService{
		cfg:       cfg,
		jobs:      make(map[string]*previewJob),
		wp:        NewWorkerPool(2, cfg.MaxQueue),
		imageProc: NewBimgProcessor(),
		pdfProc:   NewPDFProcessor(runner),
		videoProc: NewVideoProcessor(runner),
	}
	t.Cleanup(func() {
		_ = svc.wp.Stop(context.Background())
	})

	return svc
}

// writeTestInput creates a small regular file inside a fresh allowed root.
func writeTestInput(t *testing.T) (dir string, path string) {
	t.Helper()

	dir = t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	path = filepath.Join(resolved, "input.png")
	if err := os.WriteFile(path, []byte("preview-input"), 0o600); err != nil {
		t.Fatalf("write test input: %v", err)
	}

	return resolved, path
}

func testPreviewRequest(path string) PreviewRequest {
	return PreviewRequest{Path: path, Width: 64, Height: 64, Format: "auto"}
}

func waitForJobState(t *testing.T, svc *PreviewService, jobID string, want JobState) JobStatus {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, ok := svc.Status(jobID)
		if ok && status.Status == want {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}

	status, ok := svc.Status(jobID)
	t.Fatalf("job %s did not reach state %q; found=%v status=%+v", jobID, want, ok, status)
	return JobStatus{}
}

func TestEnqueueQueueFullRemovesRegistration(t *testing.T) {
	root, path := writeTestInput(t)
	svc := newTestService(t, Config{AllowedRoots: []string{root}, MaxQueue: 10})
	// Shrink only the worker queue so the service-level admission succeeds and
	// the worker-pool rejection path is exercised.
	svc.wp = NewWorkerPool(1, 1)

	firstID, err := svc.Enqueue(context.Background(), testPreviewRequest(path))
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if firstID == "" {
		t.Fatal("expected a job id")
	}

	if _, err := svc.Enqueue(context.Background(), testPreviewRequest(path)); err == nil {
		t.Fatal("expected the worker queue to reject the second job")
	}

	svc.mu.RLock()
	registered := len(svc.jobs)
	_, secondRegistered := svc.jobs[firstID]
	svc.mu.RUnlock()
	if registered != 1 || !secondRegistered {
		t.Fatalf("expected only the first job to remain registered, got %d jobs", registered)
	}
}

func TestEnqueueMaxQueueCountsOnlyActiveJobs(t *testing.T) {
	root, path := writeTestInput(t)
	svc := newTestService(t, Config{AllowedRoots: []string{root}, MaxQueue: 1})
	// No workers are started: the job stays queued. The worker queue is bigger
	// than MaxQueue so a rejection can only come from the service accounting.
	svc.wp = NewWorkerPool(2, 4)
	firstID, err := svc.Enqueue(context.Background(), testPreviewRequest(path))
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}

	if _, err := svc.Enqueue(context.Background(), testPreviewRequest(path)); err == nil {
		t.Fatal("expected the second queued job to be rejected")
	}

	// A completed job waits for TTL cleanup but no longer consumes capacity.
	svc.mu.Lock()
	svc.jobs[firstID].State = JobStateSucceeded
	svc.jobs[firstID].Result = &PreviewResult{Success: true}
	svc.mu.Unlock()

	if _, err := svc.Enqueue(context.Background(), testPreviewRequest(path)); err != nil {
		t.Fatalf("expected a completed job to free queue capacity: %v", err)
	}
}

func TestEnqueueHonorsCallerContextCancellation(t *testing.T) {
	root, path := writeTestInput(t)
	svc := newTestService(t, Config{AllowedRoots: []string{root}, MaxQueue: 4})

	started := make(chan struct{}, 1)
	svc.imageProc = &MockProcessor{
		Started: started,
		ProcessFunc: func(ctx context.Context, _ PreviewRequest) ([]byte, string, error) {
			<-ctx.Done()
			return nil, "", ctx.Err()
		},
	}
	svc.wp.Start(func(job *previewJob) {
		_ = svc.handleJob(job)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobID, err := svc.Enqueue(ctx, testPreviewRequest(path))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("processor did not start")
	}

	cancel()

	status := waitForJobState(t, svc, jobID, JobStateCanceled)
	if !strings.Contains(status.Message, "canceled") {
		t.Fatalf("expected a cancellation message, got %q", status.Message)
	}
}

func TestStatusContextRejectsCanceledContext(t *testing.T) {
	root, path := writeTestInput(t)
	svc := newTestService(t, Config{AllowedRoots: []string{root}, MaxQueue: 2})

	jobID, err := svc.Enqueue(context.Background(), testPreviewRequest(path))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, ok := svc.StatusContext(ctx, jobID); ok {
		t.Fatal("expected StatusContext to reject a canceled context")
	}
}

func TestFetchRejectsCanceledContext(t *testing.T) {
	root, path := writeTestInput(t)
	svc := newTestService(t, Config{AllowedRoots: []string{root}, MaxQueue: 2})

	jobID, err := svc.Enqueue(context.Background(), testPreviewRequest(path))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.Fetch(ctx, jobID); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
