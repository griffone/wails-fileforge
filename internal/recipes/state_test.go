package recipes

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

func seedRecipeRun(service *Service, result models.RecipeResult) {
	service.jobs[result.JobID] = &recipeJob{result: result}
}

func recipeRunIDForIndex(index int) string {
	return fmt.Sprintf("recipe_terminal_%02d", index)
}

func TestListRunsSortsNewestFirstAndAppliesLimit(t *testing.T) {
	service := NewService(registry.NewRegistry(), nil, nil)
	service.mu.Lock()
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_old", StartedAt: 100, Status: models.JobStatusSuccess})
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_tie_a", StartedAt: 200, Status: models.JobStatusFailed})
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_tie_b", StartedAt: 200, Status: models.JobStatusSuccess})
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_new", StartedAt: 300, Status: models.JobStatusRunning})
	service.mu.Unlock()

	runs := service.ListRuns(0)
	want := []string{"recipe_new", "recipe_tie_b", "recipe_tie_a", "recipe_old"}
	if len(runs) != len(want) {
		t.Fatalf("expected %v, got %+v", want, runs)
	}
	for index, jobID := range want {
		if runs[index].JobID != jobID {
			t.Fatalf("expected run %d to be %s, got %s", index, jobID, runs[index].JobID)
		}
	}

	limited := service.ListRuns(2)
	if len(limited) != 2 || limited[0].JobID != "recipe_new" || limited[1].JobID != "recipe_tie_b" {
		t.Fatalf("expected the two newest runs, got %+v", limited)
	}
}

func TestListRunsReturnsEmptyNonNilSlice(t *testing.T) {
	service := NewService(registry.NewRegistry(), nil, nil)

	runs := service.ListRuns(0)
	if runs == nil {
		t.Fatal("expected an empty non-nil slice")
	}
	if len(runs) != 0 {
		t.Fatalf("expected no runs, got %+v", runs)
	}
}

func TestTerminalRunRetentionKeepsNewestTerminalRuns(t *testing.T) {
	service := NewService(registry.NewRegistry(), nil, nil)
	service.mu.Lock()
	for index := 1; index <= retainedTerminalRuns+5; index++ {
		seedRecipeRun(service, models.RecipeResult{
			JobID:     recipeRunIDForIndex(index),
			StartedAt: int64(index),
			Status:    models.JobStatusSuccess,
		})
	}
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_active_old", StartedAt: 1, Status: models.JobStatusRunning})
	seedRecipeRun(service, models.RecipeResult{JobID: "recipe_active_queued", StartedAt: 2, Status: models.JobStatusQueued})
	service.pruneTerminalRunsLocked()
	service.mu.Unlock()

	if len(service.jobs) != retainedTerminalRuns+2 {
		t.Fatalf("expected %d retained runs, got %d", retainedTerminalRuns+2, len(service.jobs))
	}
	for index := 1; index <= 5; index++ {
		if _, found := service.jobs[recipeRunIDForIndex(index)]; found {
			t.Fatalf("expected the oldest terminal run %s to be pruned", recipeRunIDForIndex(index))
		}
	}
	if _, found := service.jobs[recipeRunIDForIndex(retainedTerminalRuns+5)]; !found {
		t.Fatalf("expected the newest terminal run %s to be retained", recipeRunIDForIndex(retainedTerminalRuns+5))
	}
	for _, jobID := range []string{"recipe_active_old", "recipe_active_queued"} {
		if _, found := service.jobs[jobID]; !found {
			t.Fatalf("expected the non-terminal run %s to survive pruning", jobID)
		}
	}
}

func TestRunPrunesOldTerminalRunsOnRegistration(t *testing.T) {
	service, _, _ := newTestService(t)

	service.mu.Lock()
	for index := 1; index <= retainedTerminalRuns+5; index++ {
		seedRecipeRun(service, models.RecipeResult{
			JobID:     recipeRunIDForIndex(index),
			StartedAt: int64(index),
			Status:    models.JobStatusSuccess,
		})
	}
	service.mu.Unlock()

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: []string{input},
		Options:    map[string]any{"targetSizeBytes": testLargeTargetBytes},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}
	waitForRecipeTerminal(t, service, run.JobID)

	service.mu.RLock()
	defer service.mu.RUnlock()

	if len(service.jobs) != retainedTerminalRuns+1 {
		t.Fatalf("expected %d retained runs after registration, got %d", retainedTerminalRuns+1, len(service.jobs))
	}
	for index := 1; index <= 5; index++ {
		if _, found := service.jobs[recipeRunIDForIndex(index)]; found {
			t.Fatalf("expected the oldest terminal run %s to be pruned on registration", recipeRunIDForIndex(index))
		}
	}
	if _, found := service.jobs[run.JobID]; !found {
		t.Fatalf("expected the newly registered run %s to be tracked", run.JobID)
	}
}

func TestRecipeStateEmitterReceivesQueuedAndTerminalSnapshots(t *testing.T) {
	service, _, _ := newTestService(t)

	snapshots := make(chan models.RecipeResult, 16)
	service.SetStateEmitter(func(result models.RecipeResult) {
		snapshots <- result
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: []string{input},
		Options:    map[string]any{"targetSizeBytes": testLargeTargetBytes},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	var queued models.RecipeResult
	select {
	case queued = <-snapshots:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the queued state snapshot")
	}
	if queued.JobID != run.JobID || queued.Status != models.JobStatusQueued {
		t.Fatalf("expected a queued snapshot for %s, got %+v", run.JobID, queued)
	}
	if queued.RecipeID != RecipeIDEmailPDF || len(queued.Steps) != 2 {
		t.Fatalf("expected the full queued run snapshot, got %+v", queued)
	}

	waitForRecipeTerminal(t, service, run.JobID)

	deadline := time.After(10 * time.Second)
	for {
		select {
		case snapshot := <-snapshots:
			if snapshot.JobID != run.JobID || !isTerminalRecipeStatus(snapshot.Status) {
				continue
			}
			if snapshot.EndedAt == 0 {
				t.Fatalf("expected a finished terminal snapshot, got %+v", snapshot)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for the terminal state snapshot")
		}
	}
}
