package automation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fileforge-desktop/internal/models"
)

func runRecord(id, ruleID, status string, at int64) models.AutomationRunRecordV1 {
	return models.AutomationRunRecordV1{
		ID:       id,
		RuleID:   ruleID,
		RuleName: "Rule " + ruleID,
		FilePath: "/tmp/in-" + id + ".txt",
		Status:   status,
		Message:  "message " + id,
		Outputs:  []string{"/tmp/out-" + id + ".txt"},
		At:       at,
	}
}

func TestRunStoreRecordOrderAndLimit(t *testing.T) {
	store := NewRunStore(filepath.Join(t.TempDir(), runsFileName))

	store.Record(runRecord("run_a", "rule_1", models.JobStatusSuccess, 100))
	store.Record(runRecord("run_b", "rule_1", models.JobStatusFailed, 200))
	store.Record(runRecord("run_c", "rule_2", models.JobStatusCancelled, 300))

	runs := store.List(2)
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].ID != "run_c" || runs[1].ID != "run_b" {
		t.Fatalf("expected newest runs first, got %+v", runs)
	}

	if all := store.List(0); len(all) != 3 {
		t.Fatalf("a zero limit must return the default page, got %d", len(all))
	}
	if negative := store.List(-5); len(negative) != 1 {
		t.Fatalf("a negative limit must clamp to one run, got %d", len(negative))
	}
	if all := store.List(500); len(all) != 3 {
		t.Fatalf("limit must clamp to the stored runs, got %d", len(all))
	}
}

func TestRunStoreGeneratesIDAndCapsOutputs(t *testing.T) {
	store := NewRunStore(filepath.Join(t.TempDir(), runsFileName))

	outputs := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		outputs = append(outputs, "/tmp/out-"+string(rune('a'+index))+".txt")
	}
	store.Record(models.AutomationRunRecordV1{
		RuleID:   " rule_1 ",
		RuleName: " Inbox ",
		FilePath: " /tmp/in.txt ",
		Status:   models.JobStatusSuccess,
		Message:  " done ",
		Outputs:  outputs,
	})

	runs := store.List(10)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	record := runs[0]
	if !strings.HasPrefix(record.ID, "run_") {
		t.Fatalf("expected a generated run id, got %q", record.ID)
	}
	if record.At <= 0 {
		t.Fatalf("expected a generated timestamp, got %d", record.At)
	}
	if record.RuleID != "rule_1" || record.RuleName != "Inbox" || record.FilePath != "/tmp/in.txt" || record.Message != "done" {
		t.Fatalf("expected trimmed fields, got %+v", record)
	}
	if len(record.Outputs) != maxRunOutputs {
		t.Fatalf("expected %d capped outputs, got %d", maxRunOutputs, len(record.Outputs))
	}
}

func TestRunStoreCapsStoredRuns(t *testing.T) {
	store := newRunStore(filepath.Join(t.TempDir(), runsFileName), 3)

	for index := 0; index < 5; index++ {
		id := "run_" + string(rune('0'+index))
		store.Record(runRecord(id, "rule_1", models.JobStatusSuccess, int64(index)))
	}

	runs := store.List(10)
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs after the cap, got %d", len(runs))
	}
	if runs[0].ID != "run_4" || runs[1].ID != "run_3" || runs[2].ID != "run_2" {
		t.Fatalf("expected newest runs first, got %+v", runs)
	}
}

func TestRunStorePersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), runsFileName)
	store := NewRunStore(path)

	store.Record(runRecord("run_a", "rule_1", models.JobStatusSuccess, 100))
	store.Record(runRecord("run_b", "rule_1", models.JobStatusPartialSuccess, 200))

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected the run history file to exist: %v", err)
	}

	reloaded := NewRunStore(path)
	runs := reloaded.List(10)
	if len(runs) != 2 {
		t.Fatalf("expected 2 reloaded runs, got %d", len(runs))
	}
	if runs[0].ID != "run_b" || runs[0].Status != models.JobStatusPartialSuccess {
		t.Fatalf("unexpected newest reloaded run: %+v", runs[0])
	}
	if runs[1].ID != "run_a" || len(runs[1].Outputs) != 1 {
		t.Fatalf("unexpected oldest reloaded run: %+v", runs[1])
	}
}

func TestRunStoreClearRemovesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), runsFileName)
	store := NewRunStore(path)

	store.Record(runRecord("run_a", "rule_1", models.JobStatusSuccess, 100))
	store.Record(runRecord("run_b", "rule_1", models.JobStatusFailed, 200))

	if err := store.Clear(); err != nil {
		t.Fatalf("clear run history: %v", err)
	}
	if runs := store.List(10); len(runs) != 0 {
		t.Fatalf("expected an empty history after clear, got %+v", runs)
	}
	if runs := NewRunStore(path).List(10); len(runs) != 0 {
		t.Fatalf("expected the cleared history to persist, got %+v", runs)
	}
}

func TestRunStoreToleratesMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), runsFileName)
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}

	store := NewRunStore(path)
	if runs := store.List(10); len(runs) != 0 {
		t.Fatalf("expected an empty history, got %+v", runs)
	}

	store.Record(runRecord("run_a", "rule_1", models.JobStatusSuccess, 100))
	if runs := NewRunStore(path).List(10); len(runs) != 1 || runs[0].ID != "run_a" {
		t.Fatalf("expected the store to recover, got %+v", runs)
	}
}

func TestDefaultRunPathUsesRunsFileName(t *testing.T) {
	path := DefaultRunPath()
	if !strings.HasSuffix(path, runsFileName) {
		t.Fatalf("unexpected default run path: %s", path)
	}
}

// TestRunStoreConcurrentRecordsKeepEveryRun proves disk writes stay ordered
// with their snapshots when fsync runs outside the state lock.
func TestRunStoreConcurrentRecordsKeepEveryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), runsFileName)
	store := NewRunStore(path)

	const count = 20
	var group sync.WaitGroup
	for index := 0; index < count; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()

			id := fmt.Sprintf("run_%d", index)
			store.Record(runRecord(id, "rule_1", models.JobStatusSuccess, int64(index)))
		}(index)
	}
	group.Wait()

	if runs := store.List(100); len(runs) != count {
		t.Fatalf("in-memory runs = %d, want %d", len(runs), count)
	}
	if runs := NewRunStore(path).List(100); len(runs) != count {
		t.Fatalf("persisted runs = %d, want %d", len(runs), count)
	}
}
