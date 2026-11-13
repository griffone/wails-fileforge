package jobs

import (
	"context"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

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

func newPlanTestOrchestrator(t *testing.T, tool *fakePlannerTool) *Orchestrator {
	t.Helper()

	reg := registry.NewRegistry()
	if err := reg.RegisterToolV2(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	return NewOrchestrator(reg, 1)
}

func TestPlanDelegatesToPlannerTool(t *testing.T) {
	planned := models.PlannedOutputV1{
		InputPath:  "/tmp/input.png",
		OutputPath: "/tmp/input.webp",
		OutputKind: "file",
	}
	tool := &fakePlannerTool{
		id: "tool.fake.plan",
		plan: models.PlanJobResponseV1{
			Success: true,
			Message: "ready",
			Outputs: []models.PlannedOutputV1{planned},
		},
	}

	orchestrator := newPlanTestOrchestrator(t, tool)
	plan := orchestrator.Plan(context.Background(), models.JobRequestV1{ToolID: tool.id, Mode: "single", InputPaths: []string{planned.InputPath}})

	if !tool.called {
		t.Fatal("expected the planner tool to be called")
	}
	if !plan.Success {
		t.Fatalf("expected successful plan, got %q", plan.Message)
	}
	if len(plan.Outputs) != 1 || plan.Outputs[0].OutputPath != planned.OutputPath {
		t.Fatalf("expected planned output %q, got %+v", planned.OutputPath, plan.Outputs)
	}
	if plan.Error != nil {
		t.Fatalf("expected no plan error, got %+v", plan.Error)
	}
}

func TestPlanReportsUnsupportedTool(t *testing.T) {
	// A plain fakeBatchTool does not implement Planner.
	batchTool := &fakeBatchTool{id: "tool.no.plan"}
	reg := registry.NewRegistry()
	if err := reg.RegisterToolV2(batchTool); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	orchestrator := NewOrchestrator(reg, 1)

	plan := orchestrator.Plan(context.Background(), models.JobRequestV1{ToolID: batchTool.id, Mode: "batch"})
	if plan.Success {
		t.Fatal("expected unsupported plan to fail")
	}
	if plan.Error == nil || plan.Error.DetailCode != "PLAN_UNSUPPORTED" {
		t.Fatalf("expected PLAN_UNSUPPORTED, got %+v", plan.Error)
	}
}

func TestPlanReportsUnknownTool(t *testing.T) {
	tool := &fakePlannerTool{id: "tool.known"}
	orchestrator := newPlanTestOrchestrator(t, tool)

	plan := orchestrator.Plan(context.Background(), models.JobRequestV1{ToolID: "tool.unknown"})
	if plan.Success {
		t.Fatal("expected unknown tool plan to fail")
	}
	if plan.Error == nil || plan.Error.DetailCode != "TOOL_NOT_FOUND" {
		t.Fatalf("expected TOOL_NOT_FOUND, got %+v", plan.Error)
	}
}

func TestPlanSurfacesPlannerError(t *testing.T) {
	tool := &fakePlannerTool{
		id:      "tool.fake.failing",
		plan:    models.PlanJobResponseV1{Message: "conflict"},
		planErr: models.NewCanonicalJobError("IMAGE_OUTPUT_EXISTS", "output already exists", nil),
	}
	orchestrator := newPlanTestOrchestrator(t, tool)

	plan := orchestrator.Plan(context.Background(), models.JobRequestV1{ToolID: tool.id, Mode: "single"})
	if plan.Success {
		t.Fatal("expected failing plan")
	}
	if plan.Error == nil {
		t.Fatal("expected a plan error")
	}
	if plan.Error.DetailCode != "IMAGE_OUTPUT_EXISTS" {
		t.Fatalf("expected detail code IMAGE_OUTPUT_EXISTS, got %q", plan.Error.DetailCode)
	}
}
