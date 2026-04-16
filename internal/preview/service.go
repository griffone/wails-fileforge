package preview

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Config holds preview service configuration used at construction.
type Config struct {
	AllowedRoots []string
	MaxQueue     int
}

// PreviewService manages preview jobs.
type PreviewService struct {
	cfg Config

	mu   sync.RWMutex
	jobs map[string]*previewJob
}

// NewPreviewService constructs a PreviewService with provided config.
func NewPreviewService(cfg Config) *PreviewService {
	return &PreviewService{
		cfg:  cfg,
		jobs: make(map[string]*previewJob),
	}
}

// Enqueue validates the request and registers a new job. Worker execution is TODO.
func (s *PreviewService) Enqueue(ctx context.Context, req PreviewRequest) (string, error) {
	// basic validation
	if err := req.Validate(); err != nil {
		return "", fmt.Errorf("preview: %w", err)
	}
	if err := validatePath(req.Path, s.cfg.AllowedRoots); err != nil {
		return "", fmt.Errorf("preview: %w", err)
	}

	// create job id (UUID creation left simple for now)
	id := fmt.Sprintf("job-%d", time.Now().UnixNano())
	job := &previewJob{
		ID:        id,
		Req:       req,
		State:     JobStateQueued,
		Progress:  0,
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.MaxQueue > 0 && len(s.jobs) >= s.cfg.MaxQueue {
		return "", fmt.Errorf("preview: queue full")
	}
	s.jobs[id] = job

	// TODO: (image-preview) start worker/queue handling. For now return queued.
	return id, nil
}

// Fetch returns the result for a job if available.
func (s *PreviewService) Fetch(ctx context.Context, jobID string) (PreviewResult, error) {
	s.mu.RLock()
	job, ok := s.jobs[jobID]
	s.mu.RUnlock()
	if !ok {
		return PreviewResult{Success: false, Message: "job not found"}, fmt.Errorf("preview: job not found")
	}
	if job.Result != nil {
		return *job.Result, nil
	}
	return PreviewResult{Success: false, Message: "not ready"}, nil
}

// Status returns job status and whether the job was found.
func (s *PreviewService) Status(jobID string) (JobStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return JobStatus{Status: JobStateFailed, Progress: 0, Message: "not found"}, false
	}
	return JobStatus{Status: job.State, Progress: job.Progress, Message: ""}, true
}

// Cancel marks a job canceled if possible.
func (s *PreviewService) Cancel(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("preview: job not found")
	}
	if job.State == JobStateSucceeded || job.State == JobStateFailed {
		return fmt.Errorf("preview: cannot cancel completed job")
	}
	job.State = JobStateCanceled
	job.Progress = 0
	job.Result = &PreviewResult{Success: false, Message: "canceled"}
	return nil
}

// Shutdown performs graceful shutdown. TODO: implement worker shutdown and cleanup.
func (s *PreviewService) Shutdown(ctx context.Context) error {
	// TODO: (image-preview) cancel workers, flush queue, persist state if needed
	done := make(chan struct{})
	go func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		// mark queued jobs as canceled on shutdown
		for _, j := range s.jobs {
			if j.State == JobStateQueued || j.State == JobStateRunning {
				j.State = JobStateCanceled
				j.Result = &PreviewResult{Success: false, Message: "shutdown"}
			}
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("preview: shutdown canceled: %w", ctx.Err())
	}
}
