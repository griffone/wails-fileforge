package jobs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"fileforge-desktop/internal/models"
)

// fakeBatchTool records every batch call and delegates execution to a test
// hook. Calls are appended from the engine execution goroutine and read after
// the job reaches a terminal state, which establishes the required ordering.
type fakeBatchTool struct {
	id      string
	calls   [][]string
	execute func(ctx context.Context, req models.JobRequestV1, call int, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1)
}

func (t *fakeBatchTool) ID() string {
	return t.id
}

func (t *fakeBatchTool) Capability() string {
	return t.id
}

func (t *fakeBatchTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *fakeBatchTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *fakeBatchTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *fakeBatchTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	t.calls = append(t.calls, append([]string(nil), req.InputPaths...))

	return t.execute(ctx, req, len(t.calls), onProgress)
}

// fakeSingleFileTool writes a real output file so history and verification
// flows observe deterministic bytes. started and waitFor let tests hold a job
// in the running state.
type fakeSingleFileTool struct {
	id         string
	outputPath string
	fail       bool
	started    chan<- struct{}
	waitFor    <-chan struct{}
}

func (t *fakeSingleFileTool) ID() string {
	return t.id
}

func (t *fakeSingleFileTool) Capability() string {
	return t.id
}

func (t *fakeSingleFileTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *fakeSingleFileTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *fakeSingleFileTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *fakeSingleFileTool) ExecuteSingle(_ context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	if t.started != nil {
		t.started <- struct{}{}
	}
	if t.waitFor != nil {
		<-t.waitFor
	}

	inputPath := ""
	if len(req.InputPaths) > 0 {
		inputPath = req.InputPaths[0]
	}
	if t.fail {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: "fake failure"},
			models.NewCanonicalJobError("FAKE_EXECUTION_FAILED", "fake failure", nil)
	}

	if err := os.MkdirAll(filepath.Dir(t.outputPath), 0o755); err != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_WRITE_FAILED", err.Error(), nil)
	}
	if err := os.WriteFile(t.outputPath, []byte("converted"), 0o644); err != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_WRITE_FAILED", err.Error(), nil)
	}

	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: t.outputPath,
		Outputs:    []string{t.outputPath},
		Success:    true,
		Message:    "fake conversion complete",
	}, nil
}

// fakePlannerTool returns a fixed plan so planner delegation and error
// propagation can be asserted without a real tool.
type fakePlannerTool struct {
	id      string
	plan    models.PlanJobResponseV1
	planErr *models.JobErrorV1
	called  bool
}

func (t *fakePlannerTool) ID() string {
	return t.id
}

func (t *fakePlannerTool) Capability() string {
	return t.id
}

func (t *fakePlannerTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *fakePlannerTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *fakePlannerTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *fakePlannerTool) Plan(_ context.Context, _ models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	t.called = true

	return t.plan, t.planErr
}

// plannedFakeTool implements both Planner and PlanExecutor so the engine's
// plan-once path can be exercised without a real tool. It records every plan it
// receives so tests can assert the engine never re-plans.
type plannedFakeTool struct {
	id       string
	manifest models.ToolManifestV1
	planHook func(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1)
	execHook func(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1)

	planCalls atomic.Int32
	execCalls atomic.Int32

	mu            sync.Mutex
	executedPlans []models.PlanJobResponseV1
}

func (t *plannedFakeTool) ID() string {
	return t.id
}

func (t *plannedFakeTool) Capability() string {
	return t.id
}

func (t *plannedFakeTool) Manifest() models.ToolManifestV1 {
	if t.manifest.ToolID == "" {
		return models.ToolManifestV1{ToolID: t.id}
	}

	return t.manifest
}

func (t *plannedFakeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *plannedFakeTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *plannedFakeTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	t.planCalls.Add(1)

	return t.planHook(ctx, req)
}

func (t *plannedFakeTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	t.execCalls.Add(1)
	t.mu.Lock()
	t.executedPlans = append(t.executedPlans, *cloneJobPlan(&plan))
	t.mu.Unlock()

	if t.execHook == nil {
		return planSuccessItems(plan), nil
	}

	return t.execHook(ctx, req, plan, onProgress)
}

func (t *plannedFakeTool) recordedPlans() []models.PlanJobResponseV1 {
	t.mu.Lock()
	defer t.mu.Unlock()

	return append([]models.PlanJobResponseV1(nil), t.executedPlans...)
}

// plannerOnlyFakeTool implements Planner and the legacy single executor but not
// PlanExecutor, proving a planned submission still runs through the old path.
type plannerOnlyFakeTool struct {
	id        string
	plan      models.PlanJobResponseV1
	calls     atomic.Int32
	planCalls atomic.Int32
}

func (t *plannerOnlyFakeTool) ID() string {
	return t.id
}

func (t *plannerOnlyFakeTool) Capability() string {
	return t.id
}

func (t *plannerOnlyFakeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *plannerOnlyFakeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *plannerOnlyFakeTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *plannerOnlyFakeTool) Plan(_ context.Context, _ models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	t.planCalls.Add(1)

	return t.plan, nil
}

func (t *plannerOnlyFakeTool) ExecuteSingle(_ context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	t.calls.Add(1)

	item := models.JobResultItemV1{InputPath: firstEngineInput(req), Success: true, Message: "legacy done"}
	if len(t.plan.Outputs) > 0 {
		item.OutputPath = t.plan.Outputs[0].OutputPath
		item.Outputs = []string{t.plan.Outputs[0].OutputPath}
		item.OutputCount = 1
	}

	return item, nil
}

// planSuccessItems builds one successful item per planned output.
func planSuccessItems(plan models.PlanJobResponseV1) []models.JobResultItemV1 {
	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	for _, output := range plan.Outputs {
		items = append(items, models.JobResultItemV1{
			InputPath:   output.InputPath,
			OutputPath:  output.OutputPath,
			Outputs:     mergePlannedOutputs(output.OutputPath),
			OutputCount: 1,
			Success:     true,
			Message:     "done",
		})
	}

	return items
}

func mergePlannedOutputs(outputPath string) []string {
	if outputPath == "" {
		return nil
	}

	return []string{outputPath}
}
