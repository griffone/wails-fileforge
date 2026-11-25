package recipes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/platform/jsonstore"
	"fileforge-desktop/internal/registry"
)

// newPersistentRecipeService builds an isolated service with a persisted run
// registry, reusing the same registry engine harness as the other recipe tests.
func newPersistentRecipeService(t *testing.T, reg *registry.Registry, path string) *Service {
	t.Helper()

	history := jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history.json"))
	service := NewServiceWithPersistence(reg, history, nil, path)
	service.SetEngine(newRecipeTestEngine(t, reg))

	return service
}

func newRecipeTestRegistry(t *testing.T) *registry.Registry {
	t.Helper()

	reg := registry.NewRegistry()
	mustRegisterTool(t, reg, pdf.NewMergeTool())
	mustRegisterTool(t, reg, pdf.NewOptimizeTool())

	return reg
}

func loadPersistedRuns(t *testing.T, path string) persistedRecipeRuns {
	t.Helper()

	var persisted persistedRecipeRuns
	if err := jsonstore.Load(path, &persisted); err != nil {
		t.Fatalf("load persisted runs: %v", err)
	}

	return persisted
}

func TestPersistenceRestoresTerminalRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "recipe_runs.json")
	reg := newRecipeTestRegistry(t)
	first := newPersistentRecipeService(t, reg, path)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	run := first.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: []string{input},
		Options:    map[string]any{"targetSizeBytes": testLargeTargetBytes},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}
	result := waitForRecipeTerminal(t, first, run.JobID)
	first.Shutdown()

	restoredService := NewServiceWithPersistence(reg, nil, nil, path)
	runs := restoredService.ListRuns(0)
	if len(runs) != 1 {
		t.Fatalf("expected one restored run, got %+v", runs)
	}

	restored := runs[0]
	if restored.JobID != result.JobID {
		t.Fatalf("expected job %s, got %s", result.JobID, restored.JobID)
	}
	if restored.Status != result.Status {
		t.Fatalf("expected status %q, got %q", result.Status, restored.Status)
	}
	if restored.RecipeID != result.RecipeID {
		t.Fatalf("expected recipe %q, got %q", result.RecipeID, restored.RecipeID)
	}
	if restored.StartedAt != result.StartedAt || restored.EndedAt != result.EndedAt {
		t.Fatalf("expected timestamps %d/%d, got %d/%d", result.StartedAt, result.EndedAt, restored.StartedAt, restored.EndedAt)
	}
	if !reflect.DeepEqual(restored.Outputs, result.Outputs) {
		t.Fatalf("expected outputs %+v, got %+v", result.Outputs, restored.Outputs)
	}
	if !reflect.DeepEqual(restored.Steps, result.Steps) {
		t.Fatalf("expected steps %+v, got %+v", result.Steps, restored.Steps)
	}
	if restored.Error != nil {
		t.Fatalf("expected no restored error, got %+v", restored.Error)
	}
}

func TestPersistenceRestoresNonTerminalRunAsInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "recipe_runs.json")
	service := NewServiceWithPersistence(registry.NewRegistry(), nil, nil, path)

	workspace := filepath.Join(t.TempDir(), "fileforge-recipe-live")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "merged.pdf"), []byte("intermediate"), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	service.mu.Lock()
	seedRecipeRun(service, models.RecipeResult{
		JobID:       "recipe_inflight",
		RecipeID:    RecipeIDEmailPDF,
		Status:      models.JobStatusRunning,
		Message:     "recipe running",
		CurrentStep: 1,
		TotalSteps:  2,
		Steps: []models.RecipeStepResult{{
			ID:     "merge",
			Name:   "Merge PDFs",
			ToolID: pdf.ToolIDPDFMerge,
			Status: recipeStepStatusPending,
		}},
		Outputs:   []string{},
		StartedAt: 200,
	})
	service.jobs["recipe_inflight"].workspace = workspace
	service.mu.Unlock()

	if err := service.saveSnapshot(true); err != nil {
		t.Fatalf("persist in-flight run: %v", err)
	}

	restoredService := NewServiceWithPersistence(registry.NewRegistry(), nil, nil, path)
	runs := restoredService.ListRuns(0)
	if len(runs) != 1 {
		t.Fatalf("expected one restored run, got %+v", runs)
	}

	restored := runs[0]
	if restored.Status != models.JobStatusInterrupted {
		t.Fatalf("expected %q, got %q", models.JobStatusInterrupted, restored.Status)
	}
	if restored.Message != recipeInterruptedMessage {
		t.Fatalf("expected message %q, got %q", recipeInterruptedMessage, restored.Message)
	}
	if restored.EndedAt == 0 {
		t.Fatal("expected an end timestamp on the interrupted run")
	}
	if restored.Error == nil || restored.Error.DetailCode != "RECIPE_INTERRUPTED" {
		t.Fatalf("expected a RECIPE_INTERRUPTED error, got %+v", restored.Error)
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the restored workspace to be removed, got err %v", err)
	}

	persisted := loadPersistedRuns(t, path)
	if len(persisted.Runs) != 1 || persisted.Runs[0].Result.Status != models.JobStatusInterrupted {
		t.Fatalf("expected the interrupted state to be persisted, got %+v", persisted.Runs)
	}
}

func TestPersistenceRetentionDropsOldestRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "recipe_runs.json")
	service := NewServiceWithPersistence(registry.NewRegistry(), nil, nil, path)

	service.mu.Lock()
	for index := 1; index <= retainedTerminalRuns+5; index++ {
		seedRecipeRun(service, models.RecipeResult{
			JobID:     recipeRunIDForIndex(index),
			StartedAt: int64(index),
			Status:    models.JobStatusSuccess,
		})
	}
	service.mu.Unlock()

	if err := service.saveSnapshot(true); err != nil {
		t.Fatalf("persist runs: %v", err)
	}

	restoredService := NewServiceWithPersistence(registry.NewRegistry(), nil, nil, path)
	runs := restoredService.ListRuns(0)
	if len(runs) != retainedTerminalRuns {
		t.Fatalf("expected %d retained runs, got %d", retainedTerminalRuns, len(runs))
	}
	if runs[0].JobID != recipeRunIDForIndex(retainedTerminalRuns+5) {
		t.Fatalf("expected the newest run first, got %s", runs[0].JobID)
	}
	for index := 1; index <= 5; index++ {
		if _, found := restoredService.jobs[recipeRunIDForIndex(index)]; found {
			t.Fatalf("expected the oldest run %s to be pruned", recipeRunIDForIndex(index))
		}
	}

	persisted := loadPersistedRuns(t, path)
	if len(persisted.Runs) != retainedTerminalRuns {
		t.Fatalf("expected %d persisted runs after recovery, got %d", retainedTerminalRuns, len(persisted.Runs))
	}
}

func TestPersistenceListRunsOrdersRestoredRunsNewestFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "recipe_runs.json")
	service := NewServiceWithPersistence(registry.NewRegistry(), nil, nil, path)

	service.mu.Lock()
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_old", StartedAt: 100, Status: models.JobStatusSuccess})
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_mid", StartedAt: 200, Status: models.JobStatusRunning})
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_new", StartedAt: 300, Status: models.JobStatusFailed})
	service.mu.Unlock()

	if err := service.saveSnapshot(true); err != nil {
		t.Fatalf("persist runs: %v", err)
	}

	restoredService := NewServiceWithPersistence(registry.NewRegistry(), nil, nil, path)
	runs := restoredService.ListRuns(2)
	want := []string{"recipe_new", "recipe_mid"}
	if len(runs) != len(want) {
		t.Fatalf("expected %v, got %+v", want, runs)
	}
	for index, jobID := range want {
		if runs[index].JobID != jobID {
			t.Fatalf("expected run %d to be %s, got %s", index, jobID, runs[index].JobID)
		}
	}
	if runs[1].Status != models.JobStatusInterrupted {
		t.Fatalf("expected the middle run to be interrupted, got %q", runs[1].Status)
	}
}

func TestPersistenceCorruptFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipe_runs.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	service := NewServiceWithPersistence(registry.NewRegistry(), nil, nil, path)
	if runs := service.ListRuns(0); len(runs) != 0 {
		t.Fatalf("expected an empty registry, got %+v", runs)
	}

	service.mu.Lock()
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_after_corrupt", StartedAt: 1, Status: models.JobStatusSuccess})
	service.mu.Unlock()
	if err := service.saveSnapshot(true); err != nil {
		t.Fatalf("expected persistence to keep working, got %v", err)
	}

	persisted := loadPersistedRuns(t, path)
	if len(persisted.Runs) != 1 || persisted.Runs[0].Result.JobID != "recipe_after_corrupt" {
		t.Fatalf("expected the registry to be rewritten, got %+v", persisted.Runs)
	}
}

func TestServiceWithoutPathDoesNotPersist(t *testing.T) {
	service := NewService(registry.NewRegistry(), nil, nil)
	if service.store != nil || service.path != "" {
		t.Fatalf("expected no persistence, got path %q and store %v", service.path, service.store)
	}

	service.mu.Lock()
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_memory_only", StartedAt: 1, Status: models.JobStatusSuccess})
	service.mu.Unlock()

	// Both paths must be safe no-ops without a store.
	service.persistRuns()
	if err := service.saveSnapshot(true); err != nil {
		t.Fatalf("expected a no-op save, got %v", err)
	}
	service.Shutdown()

	if runs := service.ListRuns(0); len(runs) != 1 {
		t.Fatalf("expected the in-memory run to survive, got %+v", runs)
	}
}

func TestIsRemovableWorkspaceGuardsPersistedPaths(t *testing.T) {
	t.Parallel()

	tempRoot := os.TempDir()
	outsideRoot := filepath.Join(filepath.Dir(filepath.Clean(tempRoot)), workspaceTempPrefix+"outside")

	cases := []struct {
		name      string
		workspace string
		want      bool
	}{
		{name: "inside temp with prefix", workspace: filepath.Join(tempRoot, workspaceTempPrefix+"abc"), want: true},
		{name: "inside temp without prefix", workspace: filepath.Join(tempRoot, "keep-me"), want: false},
		{name: "outside temp with prefix", workspace: outsideRoot, want: false},
		{name: "temp root itself", workspace: tempRoot, want: false},
		{name: "parent escape", workspace: filepath.Join(tempRoot, "..", workspaceTempPrefix+"escape"), want: false},
		{name: "empty", workspace: "  ", want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := isRemovableWorkspace(testCase.workspace); got != testCase.want {
				t.Fatalf("isRemovableWorkspace(%q) = %v, want %v", testCase.workspace, got, testCase.want)
			}
		})
	}
}

func TestCleanupRestoredWorkspaceLeavesUnrelatedDirectoriesAlone(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil, nil)
	root := t.TempDir()

	kept := filepath.Join(root, "keep-me")
	if err := os.MkdirAll(kept, 0o755); err != nil {
		t.Fatalf("create unrelated directory: %v", err)
	}
	removable := filepath.Join(root, workspaceTempPrefix+"live")
	if err := os.MkdirAll(removable, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	service.cleanupRestoredWorkspace(kept)
	service.cleanupRestoredWorkspace(removable)

	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("expected the unrelated directory to survive, got %v", err)
	}
	if _, err := os.Stat(removable); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the workspace to be removed, got %v", err)
	}
}
