package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	cachepkg "fileforge-desktop/internal/utils/cache"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

	// cleanupStop stops the background janitor; cleanupDone closes after the
	// janitor goroutine exits.
	cleanupStop chan struct{}
	cleanupDone chan struct{}
	janitorMu   sync.Mutex
	// janitorCloseOnce guarantees the stop channel is closed exactly once.
	janitorCloseOnce sync.Once

	shutdownOnce sync.Once
	shutdownErr  error

	// Processors are stateless and shared by every job of their media type so a
	// burst of preview requests does not allocate one processor per job.
	imageProc JobProcessor
	pdfProc   JobProcessor
	videoProc JobProcessor
	docProc   JobProcessor
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
	// Build the processors once. They are stateless, so sharing them across
	// workers is safe.
	runner := NewLocalExecRunner()
	svc.imageProc = NewBimgProcessor()
	svc.pdfProc = NewPDFProcessor(runner)
	svc.videoProc = NewVideoProcessor(runner)
	svc.docProc = NewDocumentProcessor(runner)
	// start worker pool with handler bound to this service
	svc.wp.Start(func(job *previewJob) {
		// ensure we don't block the worker forever
		_ = svc.handleJob(job)
	})
	return svc
}

// newJobID returns a dependency-free, unpredictable job identifier.
func newJobID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("preview: generate job id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

// contextError returns the context error when ctx is already done.
func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
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

// Enqueue validates the request and registers a new job. The job context is
// derived from the caller context so callers keep control over the job's
// lifetime.
func (s *PreviewService) Enqueue(ctx context.Context, req PreviewRequest) (string, error) {
	// basic validation
	if err := req.Validate(); err != nil {
		return "", fmt.Errorf("preview: %w", err)
	}
	if err := validatePath(req.Path, s.cfg.AllowedRoots); err != nil {
		return "", fmt.Errorf("preview: %w", err)
	}

	s.cleanupExpiredJobs()

	id, err := newJobID()
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	jobCtx, jobCancel := context.WithCancel(ctx)
	job := &previewJob{
		ID:        id,
		Req:       req,
		State:     JobStateQueued,
		Progress:  0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Ctx:       jobCtx,
		Cancel:    jobCancel,
	}

	s.mu.Lock()
	// MaxQueue bounds jobs that still need work. Completed jobs wait for TTL
	// cleanup and must not consume queue capacity.
	if s.cfg.MaxQueue > 0 && s.activeJobsLocked() >= s.cfg.MaxQueue {
		s.mu.Unlock()
		jobCancel()
		return "", fmt.Errorf("preview: queue full")
	}
	s.jobs[id] = job

	// enqueue to worker pool if available
	if s.wp != nil {
		if err := s.wp.Enqueue(job); err != nil {
			// Do not leave a failed registration behind: the caller has no way
			// to cancel a job that was never scheduled.
			delete(s.jobs, id)
			s.mu.Unlock()
			jobCancel()
			return "", err
		}
	}
	s.mu.Unlock()
	return id, nil
}

// activeJobsLocked reports how many jobs are queued or running. The caller
// must hold s.mu.
func (s *PreviewService) activeJobsLocked() int {
	active := 0
	for _, job := range s.jobs {
		if job.State == JobStateQueued || job.State == JobStateRunning {
			active++
		}
	}
	return active
}

// Fetch returns the result for a job if available.
func (s *PreviewService) Fetch(ctx context.Context, jobID string) (PreviewResult, error) {
	if err := contextError(ctx); err != nil {
		return PreviewResult{Success: false, Message: err.Error()}, err
	}

	s.mu.RLock()
	job, ok := s.jobs[jobID]
	if !ok {
		s.mu.RUnlock()
		return PreviewResult{Success: false, Message: "job not found"}, fmt.Errorf("preview: job not found")
	}
	var result *PreviewResult
	if job.Result != nil {
		copied := *job.Result
		result = &copied
	}
	cacheKey := job.CacheKey
	s.mu.RUnlock()

	if result != nil {
		return *result, nil
	}
	// attempt to read from cache by cacheKey
	if cacheKey != "" && s.cache != nil {
		if data, mime, ok, err := s.cache.Get(cacheKey); err == nil && ok {
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

	// Compute the cache key from the file identity before doing any work. A
	// previous run for the same file and render options can be served without
	// spawning pdftoppm, ffmpeg or libvips again.
	cp := job.Req.PageRange
	cacheKey := cachepkg.GeneratePreviewCacheKey(
		job.Req.Path,
		fi.ModTime().UnixNano(),
		fi.Size(),
		cachepkg.PageRange{Start: cp.Start, End: cp.End},
		job.Req.PageOffset,
		job.Req.Width,
		job.Req.Height,
		job.Req.Format,
		80,
	)
	s.mu.Lock()
	job.CacheKey = cacheKey
	s.mu.Unlock()

	if s.cache != nil {
		if data, mime, ok, cacheErr := s.cache.Get(cacheKey); cacheErr == nil && ok {
			s.mu.Lock()
			job.Result = &PreviewResult{Success: true, Data: data, ContentType: mime, Message: "ok (from cache)"}
			job.Progress = 100
			job.State = JobStateSucceeded
			job.UpdatedAt = time.Now()
			s.mu.Unlock()
			return nil
		}
	}

	// use the shared processor for this media type
	proc := s.processorFor(job.Req.Path)
	if proc == nil {
		err := fmt.Errorf("preview: no processor for %s", filepath.Ext(job.Req.Path))
		s.markJobFailed(job, err)
		return err
	}
	data, ct, perr := proc.Process(ctx, job.Req)
	if perr != nil {
		// A document without a usable preview is an expected outcome, not a
		// transient failure. Record it once and let the card hide the preview.
		if errors.Is(perr, ErrNoPreview) {
			s.markJobFailed(job, perr)
			return nil
		}
		// retry once on non-validation errors
		var vErr *ValidationError
		if !errors.As(perr, &vErr) && ctx.Err() == nil {
			s.mu.Lock()
			retry := job.Attempts == 0
			if retry {
				job.Attempts++
			}
			s.mu.Unlock()
			if retry {
				data, ct, perr = proc.Process(ctx, job.Req)
			}
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

	// Store the result under the same file-identity key computed above.
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

// processorFor selects the shared processor for a request path.
func (s *PreviewService) processorFor(path string) JobProcessor {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case ext == ".pdf":
		return s.pdfProc
	case isDocumentPreviewExtension(ext):
		return s.docProc
	case isVideoPreviewExtension(ext):
		return s.videoProc
	default:
		return s.imageProc
	}
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

// StatusContext is Status with a caller context check.
func (s *PreviewService) StatusContext(ctx context.Context, jobID string) (JobStatus, bool) {
	if err := contextError(ctx); err != nil {
		return JobStatus{Status: JobStateFailed, Progress: 0, Message: err.Error()}, false
	}
	return s.Status(jobID)
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

// Shutdown performs graceful shutdown. It is safe to call more than once; the
// first call performs the work and later calls return the same result.
func (s *PreviewService) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.shutdownErr = s.shutdown(ctx)
	})

	return s.shutdownErr
}

func (s *PreviewService) shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var firstErr error
	if s.wp != nil {
		if err := s.wp.Stop(ctx); err != nil {
			firstErr = fmt.Errorf("preview: workerpool stop: %w", err)
		}
	}
	s.stopJobJanitor()
	if s.cache != nil {
		s.cache.Close()
	}

	s.mu.Lock()
	canceled := make([]*previewJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job.State == JobStateQueued || job.State == JobStateRunning {
			canceled = append(canceled, job)
		}
	}
	s.jobs = make(map[string]*previewJob)
	s.mu.Unlock()

	// Cancel outside the lock: cancellation callbacks may observe job state.
	for _, job := range canceled {
		if job.Cancel != nil {
			job.Cancel()
		}
	}

	return firstErr
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
	done := make(chan struct{})

	s.janitorMu.Lock()
	if s.cleanupStop != nil {
		s.janitorMu.Unlock()
		return
	}
	s.cleanupStop = stop
	s.cleanupDone = done
	s.janitorMu.Unlock()

	go func() {
		defer close(done)
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

// stopJobJanitor stops the janitor and waits for its goroutine to exit, so the
// cache is never closed while a cleanup pass is still deleting entries.
func (s *PreviewService) stopJobJanitor() {
	s.janitorMu.Lock()
	stop := s.cleanupStop
	done := s.cleanupDone
	s.cleanupStop = nil
	s.cleanupDone = nil
	s.janitorMu.Unlock()

	if stop != nil {
		s.janitorCloseOnce.Do(func() {
			close(stop)
		})
	}
	if done != nil {
		<-done
	}
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
