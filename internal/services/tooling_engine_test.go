package services

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/logx"
	"fileforge-desktop/internal/registry"
)

// TestRunJobV1MapsQueueFullToSubmitError proves the new engine backpressure is
// surfaced through the unchanged RunJob response shape.
func TestRunJobV1MapsQueueFullToSubmitError(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	reg := registry.NewRegistry()
	tool := &historyFakeTool{
		id:         "tool.fake.queue",
		outputPath: filepath.Join(t.TempDir(), "out", "converted.pdf"),
		started:    started,
		release:    release,
	}
	if err := reg.RegisterToolV2(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	service := &ToolingService{
		registry: reg,
		history:  jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history_v1.json")),
	}
	service.engine = jobs.NewEngine(reg, jobs.Options{
		Limits:        jobs.Limits{Total: 1, CPU: 1, External: 1, IO: 1},
		MaxQueue:      1,
		Emitter:       service,
		OnJobComplete: service.recordTerminalJob,
		Logger:        logx.NewWithWriter(io.Discard, slog.LevelError),
	})
	t.Cleanup(func() {
		close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	})

	first := service.RunJobV1(models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-queue-a.pdf"},
	})
	if !first.Success {
		t.Fatalf("first submit = %+v, want success", first)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the first job to start")
	}

	second := service.RunJobV1(models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-queue-b.pdf"},
	})
	if !second.Success {
		t.Fatalf("second submit = %+v, want it queued", second)
	}

	third := service.RunJobV1(models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-queue-c.pdf"},
	})
	if third.Success {
		t.Fatalf("third submit = %+v, want a rejected submission", third)
	}
	if third.Status != jobs.StatusFailed {
		t.Fatalf("third status = %q, want %q", third.Status, jobs.StatusFailed)
	}
	if third.Message != "job queue is full; wait for running jobs to finish and retry" {
		t.Fatalf("third message = %q, want the queue-full guidance", third.Message)
	}
	if third.Error == nil || third.Error.DetailCode != "SUBMIT_ERROR" {
		t.Fatalf("third error = %+v, want detail code SUBMIT_ERROR", third.Error)
	}
}
