package automation

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

func TestServiceRecordsTerminalRunsAndClears(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)
	runs := NewRunStore(filepath.Join(t.TempDir(), runsFileName))
	service.SetRunStore(runs)

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRule{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})

	startService(t, service)

	path := filepath.Join(watch, "arrived.txt")
	writeTestFile(t, path, "hello")
	waitForRunCount(t, runner, 1, 3*time.Second)

	var listed models.ListAutomationRunsResponse
	waitFor(t, 3*time.Second, "the terminal run to be recorded", func() bool {
		listed = service.ListRuns(10)

		return listed.Success && len(listed.Runs) == 1
	})

	record := listed.Runs[0]
	if record.RuleID != rule.ID {
		t.Fatalf("rule id = %q, want %q", record.RuleID, rule.ID)
	}
	if record.RuleName != rule.Name {
		t.Fatalf("rule name = %q, want %q", record.RuleName, rule.Name)
	}
	if record.Status != models.JobStatusSuccess {
		t.Fatalf("status = %q, want %q", record.Status, models.JobStatusSuccess)
	}
	if record.FilePath != path {
		t.Fatalf("file path = %q, want %q", record.FilePath, path)
	}
	if record.Message == "" {
		t.Fatal("expected the terminal message to be recorded")
	}
	if len(record.Outputs) != 1 {
		t.Fatalf("expected one recorded output, got %+v", record.Outputs)
	}
	if record.At <= 0 {
		t.Fatalf("expected a recording timestamp, got %d", record.At)
	}
	if !strings.HasPrefix(record.ID, "run_") {
		t.Fatalf("expected a generated run id, got %q", record.ID)
	}

	cleared := service.ClearRuns()
	if !cleared.Success {
		t.Fatalf("clear runs failed: %s", cleared.Message)
	}
	if after := service.ListRuns(10); after.Success && len(after.Runs) != 0 {
		t.Fatalf("expected an empty history after clear, got %+v", after.Runs)
	}
	if persisted := NewRunStore(runs.path).List(10); len(persisted) != 0 {
		t.Fatalf("expected the cleared history to persist, got %+v", persisted)
	}
}

func TestServiceWithoutRunStoreReportsUnavailable(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	listed := service.ListRuns(10)
	if listed.Success || listed.Message == "" {
		t.Fatalf("expected a clear unavailable response, got %+v", listed)
	}

	cleared := service.ClearRuns()
	if cleared.Success || cleared.Message == "" {
		t.Fatalf("expected a clear unavailable response, got %+v", cleared)
	}
}
