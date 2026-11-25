package video

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

// stubVideoProgressRunner implements both runner contracts. Each call reports
// the percents configured for that call so the tool-level mapping is tested
// without a process.
type stubVideoProgressRunner struct {
	percents [][]float64
	calls    int
}

func (r *stubVideoProgressRunner) Run(_ context.Context, _ string, args []string) error {
	r.calls++
	return writeStubCommandOutput(args)
}

func (r *stubVideoProgressRunner) RunWithProgress(_ context.Context, _ string, args []string, _ float64, onProgress func(percent float64, message string)) error {
	index := r.calls
	r.calls++

	if index < len(r.percents) {
		for _, percent := range r.percents[index] {
			onProgress(percent, "")
		}
	}

	return writeStubCommandOutput(args)
}

func stubEngineSourceDuration(t *testing.T, duration float64, ok bool) {
	t.Helper()

	original := engine.SourceDurationProbe
	t.Cleanup(func() {
		engine.SourceDurationProbe = original
	})

	engine.SourceDurationProbe = func(context.Context, string) (float64, bool) {
		return duration, ok
	}
}

func TestConvertExecutePlanReportsOverallBatchPercent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	stubEngineSourceDuration(t, 10, true)
	runner := &stubVideoProgressRunner{percents: [][]float64{
		{50, 100},
		{50, 100},
	}}

	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, runner)
	req := videoToolRequest(ToolIDVideoConvert, "batch", []string{first, second}, nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	var updates []models.JobProgress
	items, execErr := tool.ExecutePlan(context.Background(), req, plan, func(update models.JobProgress) {
		updates = append(updates, update)
	})
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if len(updates) == 0 {
		t.Fatal("expected progress updates")
	}
	if updates[0].Current != 25 {
		t.Fatalf("first overall percent = %d, want 25", updates[0].Current)
	}
	if last := updates[len(updates)-1]; last.Current != 100 || last.Total != 100 {
		t.Fatalf("last update = %+v, want 100/100", last)
	}
	assertMonotonicPercentUpdates(t, updates)
}

func TestMergeExecutePlanReportsPercent(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	stubEngineSourceDuration(t, 10, true)
	runner := &stubVideoProgressRunner{percents: [][]float64{{25, 50, 75}}}

	tool := NewMergeToolWithDeps(stubRuntimeProbe{}, runner)
	req := videoToolRequest(ToolIDVideoMerge, "single", []string{first, second}, nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	var updates []models.JobProgress
	items, execErr := tool.ExecutePlan(context.Background(), req, plan, func(update models.JobProgress) {
		updates = append(updates, update)
	})
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("expected one successful item, got %+v", items)
	}

	if len(updates) != 4 {
		t.Fatalf("expected 4 updates (three ticks and completion), got %+v", updates)
	}
	for _, update := range updates {
		if update.Total != 100 {
			t.Fatalf("update %+v must use the 100 total", update)
		}
		if update.Stage != "merge.encode" {
			t.Fatalf("update %+v must carry the merge stage", update)
		}
	}
	assertMonotonicPercentUpdates(t, updates)
	if updates[0].Current != 25 || updates[len(updates)-1].Current != 100 {
		t.Fatalf("unexpected percent range: %+v", updates)
	}
}

func assertMonotonicPercentUpdates(t *testing.T, updates []models.JobProgress) {
	t.Helper()

	previous := 0
	for _, update := range updates {
		if update.Current < previous {
			t.Fatalf("progress regressed from %d to %d in %+v", previous, update.Current, updates)
		}
		if update.Current > update.Total {
			t.Fatalf("progress %+v exceeds its total", update)
		}
		previous = update.Current
	}
}

func TestConvertExecutePlanStillPublishesOnPlainRunner(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	stubEngineSourceDuration(t, 10, true)

	runner := &stubCommandRunner{}
	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, runner)
	req := videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	var updates []models.JobProgress
	items, execErr := tool.ExecutePlan(context.Background(), req, plan, func(update models.JobProgress) {
		updates = append(updates, update)
	})
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("expected one successful item, got %+v", items)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); statErr != nil {
		t.Fatalf("expected the planned output: %v", statErr)
	}
	if len(updates) != 1 || updates[0].Current != 100 {
		t.Fatalf("expected only the completion tick, got %+v", updates)
	}
}
