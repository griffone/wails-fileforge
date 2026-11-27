package pdf

import (
	"context"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

func TestMergeToolExecutePlanReportsPerInputProgress(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.pdf"),
		filepath.Join(dir, "second.pdf"),
		filepath.Join(dir, "third.pdf"),
	}
	for _, inputPath := range inputs {
		writePDFFixture(t, inputPath)
	}

	tool := NewMergeTool()
	req := mergeTestRequest(inputs, nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	updates := make([]models.JobProgress, 0, len(inputs)+2)
	items, execErr := tool.ExecutePlan(context.Background(), req, plan, func(update models.JobProgress) {
		updates = append(updates, update)
	})
	if execErr != nil {
		t.Fatalf("expected merge to succeed, got %v", execErr)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("expected one successful result item, got %+v", items)
	}

	if len(updates) == 0 {
		t.Fatal("expected progress updates")
	}

	previous := 0
	sawValidate := false
	sawCombine := false
	for _, update := range updates {
		if update.Current < previous {
			t.Fatalf("progress regressed from %d to %d: %+v", previous, update.Current, updates)
		}
		if update.Current > update.Total {
			t.Fatalf("update exceeds its total: %+v", update)
		}
		switch update.Stage {
		case engine.MergeStageValidate:
			sawValidate = true
		case engine.MergeStageCombine:
			sawCombine = true
		}
		previous = update.Current
	}
	if !sawValidate || !sawCombine {
		t.Fatalf("expected validate and combine stages, got %+v", updates)
	}

	last := updates[len(updates)-1]
	if last.Stage != engine.MergeStageDone || last.Current != len(inputs) || last.Total != len(inputs) {
		t.Fatalf("unexpected final update: %+v", last)
	}
}
