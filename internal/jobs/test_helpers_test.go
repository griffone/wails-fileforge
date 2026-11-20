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
	execute func(ctx context.Context, req models.JobRequest, call int, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError)
}

func (t *fakeBatchTool) ID() string {
	return t.id
}

func (t *fakeBatchTool) Capability() string {
	return t.id
}

func (t *fakeBatchTool) Manifest() models.ToolManifest {
	return models.ToolManifest{ToolID: t.id}
}

func (t *fakeBatchTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *fakeBatchTool) Validate(_ context.Context, _ models.JobRequest) *models.JobError {
	return nil
}

func (t *fakeBatchTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
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

func (t *fakeSingleFileTool) Manifest() models.ToolManifest {
	return models.ToolManifest{ToolID: t.id}
}

func (t *fakeSingleFileTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *fakeSingleFileTool) Validate(_ context.Context, _ models.JobRequest) *models.JobError {
	return nil
}

func (t *fakeSingleFileTool) ExecuteSingle(_ context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
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
		return models.JobResultItem{InputPath: inputPath, Success: false, Message: "fake failure"},
			models.NewCanonicalJobError("FAKE_EXECUTION_FAILED", "fake failure", nil)
	}

	if err := os.MkdirAll(filepath.Dir(t.outputPath), 0o755); err != nil {
		return models.JobResultItem{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_WRITE_FAILED", err.Error(), nil)
	}
	if err := os.WriteFile(t.outputPath, []byte("converted"), 0o644); err != nil {
		return models.JobResultItem{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_WRITE_FAILED", err.Error(), nil)
	}

	return models.JobResultItem{
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
	plan    models.PlanJobResponse
	planErr *models.JobError
	called  bool
}

func (t *fakePlannerTool) ID() string {
	return t.id
}

func (t *fakePlannerTool) Capability() string {
	return t.id
}

func (t *fakePlannerTool) Manifest() models.ToolManifest {
	return models.ToolManifest{ToolID: t.id}
}

func (t *fakePlannerTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *fakePlannerTool) Validate(_ context.Context, _ models.JobRequest) *models.JobError {
	return nil
}

func (t *fakePlannerTool) Plan(_ context.Context, _ models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	t.called = true

	return t.plan, t.planErr
}

// plannedFakeTool implements both Planner and PlanExecutor so the engine's
// plan-once path can be exercised without a real tool. It records every plan it
// receives so tests can assert the engine never re-plans.
type plannedFakeTool struct {
	id       string
	manifest models.ToolManifest
	planHook func(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError)
	execHook func(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError)

	planCalls atomic.Int32
	execCalls atomic.Int32

	mu            sync.Mutex
	executedPlans []models.PlanJobResponse
}

func (t *plannedFakeTool) ID() string {
	return t.id
}

func (t *plannedFakeTool) Capability() string {
	return t.id
}

func (t *plannedFakeTool) Manifest() models.ToolManifest {
	if t.manifest.ToolID == "" {
		return models.ToolManifest{ToolID: t.id}
	}

	return t.manifest
}

func (t *plannedFakeTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *plannedFakeTool) Validate(_ context.Context, _ models.JobRequest) *models.JobError {
	return nil
}

func (t *plannedFakeTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	t.planCalls.Add(1)

	return t.planHook(ctx, req)
}

func (t *plannedFakeTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	t.execCalls.Add(1)
	t.mu.Lock()
	t.executedPlans = append(t.executedPlans, *cloneJobPlan(&plan))
	t.mu.Unlock()

	if t.execHook == nil {
		return planSuccessItems(plan), nil
	}

	return t.execHook(ctx, req, plan, onProgress)
}

func (t *plannedFakeTool) recordedPlans() []models.PlanJobResponse {
	t.mu.Lock()
	defer t.mu.Unlock()

	return append([]models.PlanJobResponse(nil), t.executedPlans...)
}

// plannerOnlyFakeTool implements Planner and the legacy single executor but not
// PlanExecutor, proving a planned submission still runs through the old path.
type plannerOnlyFakeTool struct {
	id        string
	plan      models.PlanJobResponse
	calls     atomic.Int32
	planCalls atomic.Int32
}

func (t *plannerOnlyFakeTool) ID() string {
	return t.id
}

func (t *plannerOnlyFakeTool) Capability() string {
	return t.id
}

func (t *plannerOnlyFakeTool) Manifest() models.ToolManifest {
	return models.ToolManifest{ToolID: t.id}
}

func (t *plannerOnlyFakeTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *plannerOnlyFakeTool) Validate(_ context.Context, _ models.JobRequest) *models.JobError {
	return nil
}

func (t *plannerOnlyFakeTool) Plan(_ context.Context, _ models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	t.planCalls.Add(1)

	return t.plan, nil
}

func (t *plannerOnlyFakeTool) ExecuteSingle(_ context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	t.calls.Add(1)

	item := models.JobResultItem{InputPath: firstEngineInput(req), Success: true, Message: "legacy done"}
	if len(t.plan.Outputs) > 0 {
		item.OutputPath = t.plan.Outputs[0].OutputPath
		item.Outputs = []string{t.plan.Outputs[0].OutputPath}
		item.OutputCount = 1
	}

	return item, nil
}

// planSuccessItems builds one successful item per planned output.
func planSuccessItems(plan models.PlanJobResponse) []models.JobResultItem {
	items := make([]models.JobResultItem, 0, len(plan.Outputs))
	for _, output := range plan.Outputs {
		items = append(items, models.JobResultItem{
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
