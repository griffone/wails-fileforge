package preview

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// PreviewRequest describes the client's request to start a preview job.
type PreviewRequest struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"` // "webp", "jpeg", "auto"
}

// PreviewStartResponse is the response returned when a preview job is enqueued.
type PreviewStartResponse struct {
	Success bool   `json:"success"`
	JobID   string `json:"jobID"`
	Message string `json:"message"`
}

// PreviewResult contains the bytes (when available) and metadata for a finished job.
type PreviewResult struct {
	Success     bool   `json:"success"`
	Data        []byte `json:"data"`
	ContentType string `json:"contentType"`
	Message     string `json:"message"`
}

// JobState represents the lifecycle state of a preview job.
type JobState string

const (
	JobStateQueued    JobState = "queued"
	JobStateRunning   JobState = "running"
	JobStateSucceeded JobState = "succeeded"
	JobStateFailed    JobState = "failed"
	JobStateCanceled  JobState = "canceled"
	JobStateTimedOut  JobState = "timedout"
)

// JobStatus reports a job's current state and progress.
type JobStatus struct {
	Status   JobState `json:"status"`
	Progress int      `json:"progress"` // 0..100
	Message  string   `json:"message"`
}

// internal preview job tracked by the service. Not exported.
type previewJob struct {
	ID        string
	Req       PreviewRequest
	State     JobState
	Progress  int
	Result    *PreviewResult
	Err       error
	CreatedAt time.Time
	// runtime fields
	Ctx      context.Context
	Cancel   context.CancelFunc
	Attempts int
	// TODO: (image-preview) add worker metadata
}

// Validate performs small sanity checks on the request. It does not check the filesystem.
func (r *PreviewRequest) Validate() error {
	if r.Path == "" {
		return &ValidationError{Field: "path", Message: "path is required"}
	}
	if r.Width <= 0 || r.Width > 256 {
		return &ValidationError{Field: "width", Message: "width must be 1..256"}
	}
	if r.Height <= 0 || r.Height > 256 {
		return &ValidationError{Field: "height", Message: "height must be 1..256"}
	}
	f := strings.ToLower(r.Format)
	if f != "webp" && f != "jpeg" && f != "auto" {
		return &ValidationError{Field: "format", Message: "format must be 'webp', 'jpeg' or 'auto'"}
	}
	return nil
}

// ValidationError indicates a client-side validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (v *ValidationError) Error() string {
	return v.Field + ": " + v.Message
}

// validatePath verifies that the provided path is under one of allowedRoots.
// It resolves symlinks and compares absolute prefixes.
func validatePath(p string, allowedRoots []string) error {
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	eval, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = eval
	}
	for _, root := range allowedRoots {
		rabs, _ := filepath.Abs(root)
		if strings.HasPrefix(abs, rabs+string(filepath.Separator)) || abs == rabs {
			return nil
		}
	}
	return &ValidationError{Field: "path", Message: fmt.Sprintf("path is outside allowed roots: %s", p)}
}
