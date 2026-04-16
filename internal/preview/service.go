package preview

import (
	"context"
	"errors"
	cachepkg "fileforge-desktop/internal/utils/cache"
	"fmt"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
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
	wp   *WorkerPool
	// cache can be injected or a package-level default used
	cache *PreviewCache
}

// NewPreviewService constructs a PreviewService with provided config.
func NewPreviewService(cfg Config) *PreviewService {
	svc := &PreviewService{
		cfg:  cfg,
		jobs: make(map[string]*previewJob),
	}
	// initialize worker pool with defaults; caller can replace wp if desired
	svc.wp = NewWorkerPool(4, cfg.MaxQueue)
	// initialize cache with defaults
	// defaults: 100MB mem, spill threshold 1MB, 1GB disk, TTL 5m
	diskDir, _ := EnsureDiskDir("")
	cache, _ := NewPreviewCache(100*1024*1024, diskDir, 1*1024*1024*1024, 1*1024*1024, 5*time.Minute)
	svc.cache = cache
	// start worker pool with handler bound to this service
	svc.wp.Start(func(job *previewJob) {
		// ensure we don't block the worker forever
		_ = svc.handleJob(job)
	})
	return svc
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

	// create job id (use UUID for stable identifiers)
	id := uuid.NewString()
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

	// enqueue to worker pool if available
	if s.wp != nil {
		if err := s.wp.Enqueue(job); err != nil {
			// keep job registered but mark as rejected
			job.State = JobStateFailed
			job.Result = &PreviewResult{Success: false, Message: "queue full"}
			return "", fmt.Errorf("preview: %w", err)
		}
	}
	// TODO: (image-preview) worker execution will update job.Result when done
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
	// attempt to read from cache by cacheKey
	if job.CacheKey != "" && s.cache != nil {
		if data, mime, ok, err := s.cache.Get(job.CacheKey); err == nil && ok {
			return PreviewResult{Success: true, Data: data, ContentType: mime, Message: "ok (from cache)"}, nil
		} else if err != nil {
			// log but continue to indicate not ready
		}
	}
	return PreviewResult{Success: false, Message: "not ready"}, nil
}

// handleJob processes a preview job using the configured processor and cache.
func (s *PreviewService) handleJob(job *previewJob) error {
	// create per-job context with timeout
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	s.mu.Lock()
	// attach runtime fields
	job.Ctx = ctx
	job.Cancel = cancel
	job.State = JobStateRunning
	job.Progress = 10
	s.mu.Unlock()

	// simple MaxInputSize check (50MB)
	const MaxInputSizeMB = 50
	fi, err := os.Stat(job.Req.Path)
	if err != nil {
		s.markJobFailed(job, fmt.Errorf("preview: %w", err))
		return err
	}
	if fi.Size() > int64(MaxInputSizeMB)*1024*1024 {
		s.markJobFailed(job, fmt.Errorf("preview: %w", &ValidationError{Field: "path", Message: "file exceeds max input size"}))
		return nil
	}

	// use processor; switch to PDF processor when file is a PDF
	var proc JobProcessor
	if strings.ToLower(filepath.Ext(job.Req.Path)) == ".pdf" {
		// use the PDF processor backed by external tools
		// inject a LocalExecRunner by default
		proc = NewPDFProcessor(NewLocalExecRunner())
	} else {
		proc = NewBimgProcessor()
	}
	data, ct, perr := proc.Process(ctx, job.Req)
	if perr != nil {
		// retry once on non-validation errors
		var vErr *ValidationError
		if !errors.As(perr, &vErr) && job.Attempts == 0 {
			job.Attempts++
			data, ct, perr = proc.Process(ctx, job.Req)
		}
	}

	if perr != nil {
		s.markJobFailed(job, perr)
		return fmt.Errorf("preview: %w", perr)
	}

	// compute a deterministic cache key and store in cache if available
	// Note: processors SHOULD ideally return or accept a cache key; here we compute from request
	cp := job.Req.PageRange
	cacheKey := cachepkg.GeneratePreviewCacheKey(job.Req.Path, 0, 0, cachepkg.PageRange{Start: cp.Start, End: cp.End}, job.Req.PageOffset, job.Req.Width, job.Req.Height, job.Req.Format, 80)
	job.CacheKey = cacheKey
	if s.cache != nil {
		_ = s.cache.Put(cacheKey, data, ct)
	}

	s.mu.Lock()
	job.Result = &PreviewResult{Success: true, Data: data, ContentType: ct, Message: "ok"}
	job.Progress = 100
	job.State = JobStateSucceeded
	s.mu.Unlock()
	return nil
}

func (s *PreviewService) markJobFailed(job *previewJob, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.State = JobStateFailed
	job.Progress = 0
	job.Result = &PreviewResult{Success: false, Message: err.Error()}
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
	// signal worker pool to stop if present
	if s.wp != nil {
		if err := s.wp.Stop(ctx); err != nil {
			return fmt.Errorf("preview: workerpool stop: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// mark queued/running jobs as canceled on shutdown
	for _, j := range s.jobs {
		if j.State == JobStateQueued || j.State == JobStateRunning {
			j.State = JobStateCanceled
			j.Result = &PreviewResult{Success: false, Message: "shutdown"}
		}
	}
	return nil
}
