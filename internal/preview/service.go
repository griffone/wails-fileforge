package preview

import (
	"context"
	"errors"
	cachepkg "fileforge-desktop/internal/utils/cache"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config holds preview service configuration used at construction.
type Config struct {
	AllowedRoots    []string
	MaxQueue        int
	JobTTL          time.Duration
	CleanupInterval time.Duration
	JobTimeout      time.Duration
}

// PreviewService manages preview jobs.
type PreviewService struct {
	cfg Config

	mu   sync.RWMutex
	jobs map[string]*previewJob
	wp   *WorkerPool
	// cache can be injected or a package-level default used
	cache *PreviewCache
	// cleanupStop stops the background janitor
	cleanupStop chan struct{}
}

// NewPreviewService constructs a PreviewService with provided config.
func NewPreviewService(cfg Config) *PreviewService {
	cfg = applyPreviewDefaults(cfg)
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
	if svc.cache != nil {
		svc.cache.StartJanitor(0, 0)
	}
	svc.startJobJanitor()
	// start worker pool with handler bound to this service
	svc.wp.Start(func(job *previewJob) {
		// ensure we don't block the worker forever
		_ = svc.handleJob(job)
	})
	return svc
}

func applyPreviewDefaults(cfg Config) Config {
	if cfg.MaxQueue <= 0 {
		cfg.MaxQueue = DefaultMaxQueue
	}
	if cfg.JobTTL <= 0 {
		cfg.JobTTL = DefaultJobTTL
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = DefaultCleanupInterval
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = DefaultJobTimeout
	}
	return cfg
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

	s.cleanupExpiredJobs()

	// create job id (use UUID for stable identifiers)
	id := uuid.NewString()
	job := &previewJob{
		ID:        id,
		Req:       req,
		State:     JobStateQueued,
		Progress:  0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	jobCtx, jobCancel := context.WithCancel(context.Background())
	job.Ctx = jobCtx
	job.Cancel = jobCancel

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
			job.Err = err
			job.UpdatedAt = time.Now()
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
	if s.isJobCanceled(job) {
		return nil
	}
	// create per-job context with timeout
	baseCtx := job.Ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, s.cfg.JobTimeout)
	defer cancel()
	if ctx.Err() != nil {
		s.markJobCanceled(job, ctx.Err())
		return ctx.Err()
	}

	s.mu.Lock()
	// attach runtime fields
	job.State = JobStateRunning
	job.Progress = 10
	job.UpdatedAt = time.Now()
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

	// use processor; switch to a tool-specific processor by file extension
	var proc JobProcessor
	ext := strings.ToLower(filepath.Ext(job.Req.Path))
	switch {
	case ext == ".pdf":
		// use the PDF processor backed by external tools
		// inject a LocalExecRunner by default
		proc = NewPDFProcessor(NewLocalExecRunner())
	case isVideoPreviewExtension(ext):
		proc = NewVideoProcessor(NewLocalExecRunner())
	default:
		proc = NewBimgProcessor()
	}
	data, ct, perr := proc.Process(ctx, job.Req)
	if perr != nil {
		// retry once on non-validation errors
		var vErr *ValidationError
		if !errors.As(perr, &vErr) && job.Attempts == 0 && ctx.Err() == nil {
			job.Attempts++
			data, ct, perr = proc.Process(ctx, job.Req)
		}
	}
	if ctx.Err() != nil || s.isJobCanceled(job) {
		s.markJobCanceled(job, ctx.Err())
		return ctx.Err()
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
	job.UpdatedAt = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *PreviewService) markJobFailed(job *previewJob, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job.State = JobStateFailed
	job.Progress = 0
	job.Result = &PreviewResult{Success: false, Message: err.Error()}
	job.Err = err
	job.UpdatedAt = time.Now()
}

func (s *PreviewService) markJobCanceled(job *previewJob, err error) {
	if err == nil {
		err = context.Canceled
	}
	state := JobStateCanceled
	message := "canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		state = JobStateTimedOut
		message = "timed out"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	job.State = state
	job.Progress = 0
	job.Result = &PreviewResult{Success: false, Message: message}
	job.Err = err
	job.UpdatedAt = time.Now()
}

// Status returns job status and whether the job was found.
func (s *PreviewService) Status(jobID string) (JobStatus, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return JobStatus{Status: JobStateFailed, Progress: 0, Message: "not found"}, false
	}
	message := ""
	if job.Result != nil && job.Result.Message != "" {
		message = job.Result.Message
	} else if job.Err != nil {
		message = job.Err.Error()
	}

	return JobStatus{Status: job.State, Progress: job.Progress, Message: message}, true
}

// Cancel marks a job canceled if possible.
func (s *PreviewService) Cancel(jobID string) error {
	s.mu.Lock()
	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("preview: job not found")
	}
	if job.State == JobStateSucceeded || job.State == JobStateFailed {
		s.mu.Unlock()
		return fmt.Errorf("preview: cannot cancel completed job")
	}
	job.State = JobStateCanceled
	job.Progress = 0
	job.Result = &PreviewResult{Success: false, Message: "canceled"}
	job.Err = context.Canceled
	job.UpdatedAt = time.Now()
	cancel := job.Cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
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
	if s.cleanupStop != nil {
		close(s.cleanupStop)
		s.cleanupStop = nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// mark queued/running jobs as canceled on shutdown
	for _, j := range s.jobs {
		if j.State == JobStateQueued || j.State == JobStateRunning {
			j.State = JobStateCanceled
			j.Result = &PreviewResult{Success: false, Message: "shutdown"}
			j.Err = context.Canceled
			j.UpdatedAt = time.Now()
			if j.Cancel != nil {
				j.Cancel()
			}
		}
	}
	return nil
}

func (s *PreviewService) startJobJanitor() {
	if s.cfg.JobTTL <= 0 {
		return
	}
	interval := s.cfg.CleanupInterval
	if interval <= 0 {
		interval = DefaultCleanupInterval
	}
	stop := make(chan struct{})
	s.cleanupStop = stop
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.cleanupExpiredJobs()
			case <-stop:
				return
			}
		}
	}()
}

func (s *PreviewService) cleanupExpiredJobs() {
	if s.cfg.JobTTL <= 0 {
		return
	}
	cutoff := time.Now().Add(-s.cfg.JobTTL)
	cacheKeys := make([]string, 0)
	s.mu.Lock()
	for id, job := range s.jobs {
		last := job.UpdatedAt
		if last.IsZero() {
			last = job.CreatedAt
		}
		if last.After(cutoff) {
			continue
		}
		if job.State == JobStateRunning {
			continue
		}
		if job.State == JobStateQueued {
			job.State = JobStateTimedOut
			job.Progress = 0
			job.Result = &PreviewResult{Success: false, Message: "expired"}
			job.Err = context.DeadlineExceeded
			job.UpdatedAt = time.Now()
			if job.Cancel != nil {
				job.Cancel()
			}
		}
		if job.CacheKey != "" {
			cacheKeys = append(cacheKeys, job.CacheKey)
		}
		delete(s.jobs, id)
	}
	s.mu.Unlock()
	if s.cache != nil {
		for _, key := range cacheKeys {
			s.cache.Delete(key)
		}
	}
}

func (s *PreviewService) isJobCanceled(job *previewJob) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return job.State == JobStateCanceled || job.State == JobStateTimedOut
}
