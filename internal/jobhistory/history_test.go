package jobhistory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()

	dir := t.TempDir()
	return NewStore(filepath.Join(dir, "fileforge", "job_history.json")), dir
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(content)
}

func terminalJob(jobID, toolID, status string, outputs ...string) models.JobResult {
	items := make([]models.JobResultItem, 0, len(outputs))
	for _, output := range outputs {
		items = append(items, models.JobResultItem{
			Success:    true,
			OutputPath: output,
			Outputs:    []string{output},
		})
	}

	return models.JobResult{
		JobID:     jobID,
		ToolID:    toolID,
		Status:    status,
		EndedAt:   1700000000000,
		Items:     items,
		StartedAt: 1699999999000,
	}
}

func TestRecordJobFingerprintsSuccessfulOutputs(t *testing.T) {
	store, dir := newTestStore(t)
	pathA := filepath.Join(dir, "out", "a.pdf")
	pathB := filepath.Join(dir, "out", "b.pdf")
	writeTestFile(t, pathA, "alpha")
	writeTestFile(t, pathB, "beta-content")

	job := terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, pathA, pathB)
	job.Items = append(job.Items,
		models.JobResultItem{Success: true, OutputPath: pathA},
		models.JobResultItem{Success: false, OutputPath: pathB},
	)

	store.RecordJob(job)

	entries := store.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.JobID != "job_1" || entry.ToolID != "tool.pdf.merge" {
		t.Fatalf("unexpected entry identity: %+v", entry)
	}
	if entry.CreatedAt != job.EndedAt {
		t.Fatalf("expected createdAt %d, got %d", job.EndedAt, entry.CreatedAt)
	}
	if entry.Undone || entry.TrashEmptied {
		t.Fatalf("expected a fresh entry, got %+v", entry)
	}
	if len(entry.Outputs) != 2 {
		t.Fatalf("expected 2 deduped outputs, got %d: %+v", len(entry.Outputs), entry.Outputs)
	}

	wantSizes := map[string]int64{pathA: int64(len("alpha")), pathB: int64(len("beta-content"))}
	for _, output := range entry.Outputs {
		if output.SizeBytes != wantSizes[output.Path] {
			t.Fatalf("output %s: expected size %d, got %d", output.Path, wantSizes[output.Path], output.SizeBytes)
		}
		if output.ModifiedAt <= 0 {
			t.Fatalf("output %s: expected a modification time, got %d", output.Path, output.ModifiedAt)
		}
		if output.TrashedPath != "" || output.Undone {
			t.Fatalf("output %s: expected untouched record, got %+v", output.Path, output)
		}
	}
}

func TestRecordJobSkipsNonTerminalJobsAndJobsWithoutOutputs(t *testing.T) {
	store, dir := newTestStore(t)
	path := filepath.Join(dir, "out.pdf")
	writeTestFile(t, path, "content")

	for _, status := range []string{models.JobStatusQueued, models.JobStatusRunning, "", "unknown"} {
		store.RecordJob(terminalJob("job_"+status, "tool.pdf.merge", status, path))
	}
	if entries := store.List(); len(entries) != 0 {
		t.Fatalf("expected non-terminal jobs to be ignored, got %+v", entries)
	}

	missing := filepath.Join(dir, "missing.pdf")
	store.RecordJob(terminalJob("job_missing", "tool.pdf.merge", models.JobStatusSuccess, missing))
	if entries := store.List(); len(entries) != 0 {
		t.Fatalf("expected missing outputs to be skipped, got %+v", entries)
	}

	store.RecordJob(terminalJob("", "tool.pdf.merge", models.JobStatusSuccess, path))
	if entries := store.List(); len(entries) != 0 {
		t.Fatalf("expected empty job ids to be ignored, got %+v", entries)
	}
}

func TestRecordJobIsIdempotentAndListsNewestFirst(t *testing.T) {
	store, dir := newTestStore(t)
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestFile(t, first, "one")
	writeTestFile(t, second, "two")

	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, first))
	store.RecordJob(terminalJob("job_2", "tool.pdf.optimize", models.JobStatusPartialSuccess, second))
	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, first, second))

	entries := store.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].JobID != "job_2" || entries[1].JobID != "job_1" {
		t.Fatalf("expected newest-first order [job_2 job_1], got [%s %s]", entries[0].JobID, entries[1].JobID)
	}
	if len(entries[1].Outputs) != 1 {
		t.Fatalf("expected the duplicate job id to keep its original outputs, got %+v", entries[1].Outputs)
	}
}

func TestUndoMovesUntouchedOutputsToTrash(t *testing.T) {
	store, dir := newTestStore(t)
	pathA := filepath.Join(dir, "out", "a.pdf")
	pathB := filepath.Join(dir, "out", "b.pdf")
	writeTestFile(t, pathA, "alpha")
	writeTestFile(t, pathB, "beta")

	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, pathA, pathB))

	result := store.Undo(" job_1 ")
	if !result.Success {
		t.Fatalf("expected undo success, got %+v", result)
	}
	if len(result.Moved) != 2 || len(result.Skipped) != 0 {
		t.Fatalf("expected 2 moved and 0 skipped, got %+v", result)
	}
	if !strings.Contains(result.Message, "2") {
		t.Fatalf("expected the message to report the moved count, got %q", result.Message)
	}

	entry := store.List()[0]
	if !entry.Undone {
		t.Fatalf("expected the entry to be marked undone, got %+v", entry)
	}
	for _, output := range entry.Outputs {
		if _, err := os.Stat(output.Path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to leave its original path, got %v", output.Path, err)
		}
		if !output.Undone {
			t.Fatalf("expected output %s to be marked undone", output.Path)
		}
		wantTrash := filepath.Join(dir, "fileforge", "trash", "job_1")
		if !strings.HasPrefix(output.TrashedPath, wantTrash) {
			t.Fatalf("expected trash path under %s, got %s", wantTrash, output.TrashedPath)
		}
		if _, err := os.Stat(output.TrashedPath); err != nil {
			t.Fatalf("expected the trashed copy to exist: %v", err)
		}
	}
	if got := readTestFile(t, entry.Outputs[1].TrashedPath); got != "beta" {
		t.Fatalf("expected the trashed bytes to be preserved, got %q", got)
	}

	reloaded := NewStore(filepath.Join(dir, "fileforge", "job_history.json"))
	reloadedEntries := reloaded.List()
	if len(reloadedEntries) != 1 || !reloadedEntries[0].Undone {
		t.Fatalf("expected the undone state to survive reload, got %+v", reloadedEntries)
	}
}

func TestUndoSkipsModifiedFile(t *testing.T) {
	store, dir := newTestStore(t)
	pathA := filepath.Join(dir, "out", "a.pdf")
	pathB := filepath.Join(dir, "out", "b.pdf")
	writeTestFile(t, pathA, "alpha")
	writeTestFile(t, pathB, "beta")

	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, pathA, pathB))
	writeTestFile(t, pathB, "beta changed by the user")

	result := store.Undo("job_1")
	if !result.Success {
		t.Fatalf("expected the untouched output to move, got %+v", result)
	}
	if len(result.Moved) != 1 || result.Moved[0] != pathA {
		t.Fatalf("expected only %s to move, got %+v", pathA, result.Moved)
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("expected 1 skipped output, got %+v", result.Skipped)
	}
	if result.Skipped[0].Path != pathB || result.Skipped[0].Reason != reasonModified {
		t.Fatalf("expected %s to be skipped as modified, got %+v", pathB, result.Skipped[0])
	}
	if got := readTestFile(t, pathB); got != "beta changed by the user" {
		t.Fatalf("expected the modified file to be untouched, got %q", got)
	}

	entry := store.List()[0]
	if entry.Undone {
		t.Fatal("expected a partially undone entry to stay not undone")
	}

	second := store.Undo("job_1")
	if second.Success {
		t.Fatalf("expected the second undo to fail, got %+v", second)
	}
	if len(second.Moved) != 0 || len(second.Skipped) != 1 {
		t.Fatalf("expected the modified file to be skipped again, got %+v", second)
	}
}

func TestUndoSkipsMissingFile(t *testing.T) {
	store, dir := newTestStore(t)
	path := filepath.Join(dir, "out.pdf")
	writeTestFile(t, path, "content")

	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, path))
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove output: %v", err)
	}

	result := store.Undo("job_1")
	if result.Success {
		t.Fatalf("expected undo to fail, got %+v", result)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != reasonMissing {
		t.Fatalf("expected the missing output to be reported, got %+v", result.Skipped)
	}
	if result.Moved == nil || result.Skipped == nil {
		t.Fatal("expected non-nil moved and skipped slices for JSON consumers")
	}
}

func TestUndoUnknownAndAlreadyUndoneJobs(t *testing.T) {
	store, dir := newTestStore(t)
	path := filepath.Join(dir, "out.pdf")
	writeTestFile(t, path, "content")
	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, path))

	unknown := store.Undo("job_missing")
	if unknown.Success || !strings.Contains(unknown.Message, "not in history") {
		t.Fatalf("expected an unknown job to be reported, got %+v", unknown)
	}

	if result := store.Undo("job_1"); !result.Success {
		t.Fatalf("expected the first undo to succeed, got %+v", result)
	}
	again := store.Undo("job_1")
	if !again.Success || again.Message != "job is already undone" {
		t.Fatalf("expected an already-undone report, got %+v", again)
	}
	if len(again.Moved) != 0 {
		t.Fatalf("expected no additional moves, got %+v", again.Moved)
	}
}

func TestRestoreMovesOutputsBack(t *testing.T) {
	store, dir := newTestStore(t)
	pathA := filepath.Join(dir, "out", "a.pdf")
	pathB := filepath.Join(dir, "out", "b.pdf")
	writeTestFile(t, pathA, "alpha")
	writeTestFile(t, pathB, "beta")

	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, pathA, pathB))
	if result := store.Undo("job_1"); !result.Success {
		t.Fatalf("undo failed: %+v", result)
	}

	result := store.Restore("job_1")
	if !result.Success {
		t.Fatalf("expected restore success, got %+v", result)
	}
	if len(result.Moved) != 2 || len(result.Skipped) != 0 {
		t.Fatalf("expected 2 restored files, got %+v", result)
	}
	if got := readTestFile(t, pathA); got != "alpha" {
		t.Fatalf("expected original bytes at %s, got %q", pathA, got)
	}
	if got := readTestFile(t, pathB); got != "beta" {
		t.Fatalf("expected original bytes at %s, got %q", pathB, got)
	}

	entry := store.List()[0]
	if entry.Undone {
		t.Fatal("expected the entry to be marked not undone after restore")
	}
	for _, output := range entry.Outputs {
		if output.Undone || output.TrashedPath != "" {
			t.Fatalf("expected cleared undo state, got %+v", output)
		}
	}

	again := store.Restore("job_1")
	if again.Success || again.Message != "job has nothing to restore" {
		t.Fatalf("expected nothing-to-restore, got %+v", again)
	}
}

func TestRestoreSkipsOccupiedDestination(t *testing.T) {
	store, dir := newTestStore(t)
	path := filepath.Join(dir, "out.pdf")
	writeTestFile(t, path, "produced")

	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, path))
	if result := store.Undo("job_1"); !result.Success {
		t.Fatalf("undo failed: %+v", result)
	}
	trashed := store.List()[0].Outputs[0].TrashedPath

	writeTestFile(t, path, "created by the user afterwards")

	result := store.Restore("job_1")
	if result.Success {
		t.Fatalf("expected restore to refuse an occupied destination, got %+v", result)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != "destination already exists" {
		t.Fatalf("expected a destination conflict, got %+v", result.Skipped)
	}
	if got := readTestFile(t, path); got != "created by the user afterwards" {
		t.Fatalf("expected the new destination file to be untouched, got %q", got)
	}
	if got := readTestFile(t, trashed); got != "produced" {
		t.Fatalf("expected the trashed copy to stay in the trash, got %q", got)
	}
	if !store.List()[0].Undone {
		t.Fatal("expected the entry to stay undone when restore is refused")
	}
}

func TestEmptyTrashClearsTrashStateAndKeepsHistory(t *testing.T) {
	store, dir := newTestStore(t)
	path := filepath.Join(dir, "out.pdf")
	writeTestFile(t, path, "content")

	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, path))
	if result := store.Undo("job_1"); !result.Success {
		t.Fatalf("undo failed: %+v", result)
	}

	if err := store.EmptyTrash(); err != nil {
		t.Fatalf("empty trash: %v", err)
	}
	trashRoot := filepath.Join(dir, "fileforge", "trash")
	if _, err := os.Stat(trashRoot); !os.IsNotExist(err) {
		t.Fatalf("expected the trash directory to be removed, got %v", err)
	}

	entry := store.List()[0]
	if !entry.Undone || !entry.TrashEmptied {
		t.Fatalf("expected the entry to stay undone and be marked trashEmptied, got %+v", entry)
	}
	for _, output := range entry.Outputs {
		if !output.Undone || output.TrashedPath != "" {
			t.Fatalf("expected cleared trash paths, got %+v", output)
		}
	}

	if err := store.EmptyTrash(); err != nil {
		t.Fatalf("expected empty trash to be idempotent, got %v", err)
	}

	restore := store.Restore("job_1")
	if restore.Success || !strings.Contains(restore.Message, "emptied") {
		t.Fatalf("expected an explicit emptied-trash message, got %+v", restore)
	}
}

func TestHistorySurvivesReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fileforge", "job_history.json")
	output := filepath.Join(dir, "out.pdf")
	writeTestFile(t, output, "content")

	store := NewStore(path)
	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, output))

	reloaded := NewStore(path)
	entries := reloaded.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 persisted entry, got %d", len(entries))
	}
	if entries[0].JobID != "job_1" || len(entries[0].Outputs) != 1 {
		t.Fatalf("unexpected reloaded entry: %+v", entries[0])
	}
	if entries[0].Outputs[0].Path != output {
		t.Fatalf("expected output %s, got %s", output, entries[0].Outputs[0].Path)
	}
}

func TestRestoreWorksAfterReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fileforge", "job_history.json")
	output := filepath.Join(dir, "out.pdf")
	writeTestFile(t, output, "content")

	store := NewStore(path)
	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, output))
	if result := store.Undo("job_1"); !result.Success {
		t.Fatalf("undo failed: %+v", result)
	}

	reloaded := NewStore(path)
	result := reloaded.Restore("job_1")
	if !result.Success {
		t.Fatalf("expected restore to survive a restart, got %+v", result)
	}
	if got := readTestFile(t, output); got != "content" {
		t.Fatalf("expected restored bytes, got %q", got)
	}
}

func TestMalformedHistoryIsTolerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fileforge", "job_history.json")
	writeTestFile(t, path, "{not valid json")

	store := NewStore(path)
	if entries := store.List(); len(entries) != 0 {
		t.Fatalf("expected malformed history to reset, got %+v", entries)
	}

	output := filepath.Join(dir, "out.pdf")
	writeTestFile(t, output, "content")
	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, output))

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rewritten history: %v", err)
	}
	persisted := make([]models.JobHistoryEntry, 0)
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("expected valid JSON after rewrite, got %v", err)
	}
	if len(persisted) != 1 || persisted[0].JobID != "job_1" {
		t.Fatalf("unexpected rewritten history: %+v", persisted)
	}
}

func TestCapEntriesPrunesOldest(t *testing.T) {
	dir := t.TempDir()
	store := newStore(filepath.Join(dir, "fileforge", "job_history.json"), 3, 1000)

	for index := 1; index <= 5; index++ {
		output := filepath.Join(dir, fmt.Sprintf("out-%d.pdf", index))
		writeTestFile(t, output, "content")
		store.RecordJob(terminalJob(fmt.Sprintf("job_%d", index), "tool.pdf.merge", models.JobStatusSuccess, output))
	}

	entries := store.List()
	if len(entries) != 3 {
		t.Fatalf("expected 3 capped entries, got %d: %+v", len(entries), entries)
	}
	wantOrder := []string{"job_5", "job_4", "job_3"}
	for index, want := range wantOrder {
		if entries[index].JobID != want {
			t.Fatalf("entry %d: expected %s, got %s", index, want, entries[index].JobID)
		}
	}
}

func TestCapOutputFilesPrunesOldestEntries(t *testing.T) {
	dir := t.TempDir()
	store := newStore(filepath.Join(dir, "fileforge", "job_history.json"), 50, 3)

	for index := 1; index <= 3; index++ {
		first := filepath.Join(dir, fmt.Sprintf("a-%d.pdf", index))
		second := filepath.Join(dir, fmt.Sprintf("b-%d.pdf", index))
		writeTestFile(t, first, "content")
		writeTestFile(t, second, "content")
		store.RecordJob(terminalJob(fmt.Sprintf("job_%d", index), "tool.pdf.merge", models.JobStatusSuccess, first, second))
	}

	entries := store.List()
	if len(entries) != 1 {
		t.Fatalf("expected the output cap to prune old entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].JobID != "job_3" {
		t.Fatalf("expected the newest entry to survive, got %s", entries[0].JobID)
	}
	if len(entries[0].Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(entries[0].Outputs))
	}
}

func TestRecordOutputsRecordsRecipeJobs(t *testing.T) {
	store, dir := newTestStore(t)
	output := filepath.Join(dir, "recipe.pdf")
	writeTestFile(t, output, "content")

	store.RecordOutputs("recipe_1", "recipe.email_pdf", []string{output, output, "  "})

	entries := store.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 recipe entry, got %d", len(entries))
	}
	if entries[0].JobID != "recipe_1" || entries[0].ToolID != "recipe.email_pdf" {
		t.Fatalf("unexpected recipe entry: %+v", entries[0])
	}
	if len(entries[0].Outputs) != 1 {
		t.Fatalf("expected deduped outputs, got %+v", entries[0].Outputs)
	}
}

// TestRecordMovesUndoAndRestore proves a moved file is recorded with its
// destination as the undo target and its source as the restore target, so the
// existing Undo/Restore path reverts a move instead of deleting a file.
func TestRecordMovesUndoAndRestore(t *testing.T) {
	store, dir := newTestStore(t)
	source := filepath.Join(dir, "watch", "report.pdf")
	destination := filepath.Join(dir, "organized", "2026", "report.pdf")
	writeTestFile(t, destination, "moved bytes")

	store.RecordMoves("automation_rule_1", "automation.organize", []MoveRecord{{
		Source:      source,
		Destination: destination,
	}})

	entries := store.List()
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %+v", entries)
	}
	entry := entries[0]
	if len(entry.Outputs) != 1 {
		t.Fatalf("expected one output, got %+v", entry.Outputs)
	}
	output := entry.Outputs[0]
	if output.Path != destination || output.RestorePath != source {
		t.Fatalf("unexpected move record: %+v", output)
	}
	if output.SizeBytes != int64(len("moved bytes")) {
		t.Fatalf("move record must fingerprint the destination: %+v", output)
	}

	if result := store.Undo(entry.JobID); !result.Success || len(result.Moved) != 1 {
		t.Fatalf("undo failed: %+v", result)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("undo must move the destination to the trash, got err=%v", err)
	}

	result := store.Restore(entry.JobID)
	if !result.Success || len(result.Moved) != 1 || result.Moved[0] != source {
		t.Fatalf("restore must return the file to its source, got %+v", result)
	}
	if got := readTestFile(t, source); got != "moved bytes" {
		t.Fatalf("expected original bytes at the source, got %q", got)
	}

	// A reload keeps the restore target.
	reloaded := NewStore(store.path)
	stored, found := reloaded.Get(entry.JobID)
	if !found || stored.Outputs[0].RestorePath != source {
		t.Fatalf("restore path did not survive a reload: %+v", stored)
	}
}

func TestRecordMovesSkipsMissingDestinations(t *testing.T) {
	store, dir := newTestStore(t)

	store.RecordMoves("automation_rule_1", "automation.organize", []MoveRecord{{
		Source:      filepath.Join(dir, "watch", "missing.pdf"),
		Destination: filepath.Join(dir, "organized", "missing.pdf"),
	}})

	if entries := store.List(); len(entries) != 0 {
		t.Fatalf("a move whose destination is missing must not be recorded: %+v", entries)
	}
}

func TestMoveFileFallsBackToCopyOnCrossDevice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.pdf")
	dst := filepath.Join(dir, "trash", "dst.pdf")
	writeTestFile(t, src, "payload")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("create trash dir: %v", err)
	}

	modified := time.UnixMilli(1700000000123)
	if err := os.Chtimes(src, modified, modified); err != nil {
		t.Fatalf("set mtime: %v", err)
	}
	if err := os.Chmod(src, 0o640); err != nil {
		t.Fatalf("set mode: %v", err)
	}

	crossDevice := func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
	}
	if err := moveFile(src, dst, crossDevice); err != nil {
		t.Fatalf("expected the copy fallback to succeed, got %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected the source to be removed, got %v", err)
	}
	if got := readTestFile(t, dst); got != "payload" {
		t.Fatalf("expected the payload to be copied, got %q", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if info.ModTime().UnixMilli() != modified.UnixMilli() {
		t.Fatalf("expected the mtime to be preserved, got %d", info.ModTime().UnixMilli())
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("expected permissions to be preserved, got %v", info.Mode().Perm())
	}
}

func TestUndoUsesCopyFallbackWhenRenameFails(t *testing.T) {
	store, dir := newTestStore(t)
	store.rename = func(oldPath, newPath string) error {
		return &os.LinkError{Op: "rename", Old: oldPath, New: newPath, Err: syscall.EXDEV}
	}

	path := filepath.Join(dir, "out.pdf")
	writeTestFile(t, path, "content")
	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, path))

	result := store.Undo("job_1")
	if !result.Success {
		t.Fatalf("expected the fallback move to succeed, got %+v", result)
	}
	entry := store.List()[0]
	if got := readTestFile(t, entry.Outputs[0].TrashedPath); got != "content" {
		t.Fatalf("expected the trashed copy, got %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected the original path to be vacated, got %v", err)
	}
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	store, dir := newTestStore(t)
	const jobCount = 20

	var wait sync.WaitGroup
	for index := 0; index < jobCount; index++ {
		output := filepath.Join(dir, fmt.Sprintf("out-%d.pdf", index))
		writeTestFile(t, output, "content")

		wait.Add(1)
		go func(index int, output string) {
			defer wait.Done()
			store.RecordJob(terminalJob(fmt.Sprintf("job_%d", index), "tool.pdf.merge", models.JobStatusSuccess, output))
			_ = store.List()
		}(index, output)
	}
	wait.Wait()

	if entries := store.List(); len(entries) != jobCount {
		t.Fatalf("expected %d entries, got %d", jobCount, len(entries))
	}
}

// TestListIsNotBlockedBySlowUndo proves the data lock is not held during file
// moves: List must answer while Undo is stuck renaming an output.
func TestListIsNotBlockedBySlowUndo(t *testing.T) {
	store, dir := newTestStore(t)
	path := filepath.Join(dir, "out.pdf")
	writeTestFile(t, path, "content")
	store.RecordJob(terminalJob("job_1", "tool.pdf.merge", models.JobStatusSuccess, path))

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseRename := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRename)

	store.rename = func(oldPath, newPath string) error {
		close(started)
		<-release
		return os.Rename(oldPath, newPath)
	}

	undoDone := make(chan models.UndoResult, 1)
	go func() {
		undoDone <- store.Undo("job_1")
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Undo to start moving the output")
	}

	listed := make(chan struct{})
	go func() {
		if entries := store.List(); len(entries) != 1 {
			t.Errorf("List returned %d entries during Undo, want 1", len(entries))
		}
		close(listed)
	}()

	select {
	case <-listed:
	case <-time.After(time.Second):
		t.Fatal("List blocked while Undo was moving a file")
	}

	releaseRename()
	result := <-undoDone
	if !result.Success {
		t.Fatalf("expected undo success, got %+v", result)
	}
}

// TestRecordJobAndListAreSafeConcurrently exercises the snapshot writer and the
// read path under the race detector.
func TestRecordJobAndListAreSafeConcurrently(t *testing.T) {
	store, dir := newTestStore(t)
	const writers = 10

	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
				if entries := store.List(); entries == nil {
					t.Error("List returned nil")
					return
				}
			}
		}
	}()

	var wait sync.WaitGroup
	for index := 0; index < writers; index++ {
		output := filepath.Join(dir, fmt.Sprintf("out-%d.pdf", index))
		writeTestFile(t, output, "content")

		wait.Add(1)
		go func(index int, output string) {
			defer wait.Done()
			store.RecordJob(terminalJob(fmt.Sprintf("job_%d", index), "tool.pdf.merge", models.JobStatusSuccess, output))
		}(index, output)
	}
	wait.Wait()
	close(stop)
	<-readerDone

	if entries := store.List(); len(entries) != writers {
		t.Fatalf("expected %d entries, got %d", writers, len(entries))
	}
}

func TestGetReturnsDeepCopy(t *testing.T) {
	store, dir := newTestStore(t)
	output := filepath.Join(dir, "out.pdf")
	writeTestFile(t, output, "content")
	store.RecordOutputs("job_1", "tool.pdf.merge", []string{output})

	entry, found := store.Get(" job_1 ")
	if !found {
		t.Fatal("expected the recorded job to be found")
	}
	if entry.JobID != "job_1" || len(entry.Outputs) != 1 {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	entry.Outputs[0].Path = "mutated"

	stored, _ := store.Get("job_1")
	if stored.Outputs[0].Path != output {
		t.Fatalf("expected Get to return a deep copy, got %q", stored.Outputs[0].Path)
	}
	if _, found := store.Get("missing"); found {
		t.Fatal("expected a missing job to report not found")
	}
}

func TestTrashRetentionPrunesExpiredDirectories(t *testing.T) {
	store, dir := newTestStore(t)
	oldPath := filepath.Join(dir, "out", "old.pdf")
	newPath := filepath.Join(dir, "out", "new.pdf")
	writeTestFile(t, oldPath, "old")
	writeTestFile(t, newPath, "new")

	store.RecordJob(terminalJob("job_old", "tool.pdf.merge", models.JobStatusSuccess, oldPath))
	if result := store.Undo("job_old"); !result.Success {
		t.Fatalf("undo old job: %+v", result)
	}
	oldEntry, _ := store.Get("job_old")
	oldTrashDir := filepath.Dir(oldEntry.Outputs[0].TrashedPath)
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldTrashDir, oldTime, oldTime); err != nil {
		t.Fatalf("backdate trash directory: %v", err)
	}

	store.SetTrashRetention(0, 24*time.Hour)

	store.RecordJob(terminalJob("job_new", "tool.pdf.merge", models.JobStatusSuccess, newPath))
	if result := store.Undo("job_new"); !result.Success {
		t.Fatalf("undo new job: %+v", result)
	}

	if _, err := os.Stat(oldTrashDir); !os.IsNotExist(err) {
		t.Fatalf("expected the expired trash directory to be pruned, got %v", err)
	}

	pruned, _ := store.Get("job_old")
	if len(pruned.Outputs) != 1 {
		t.Fatalf("expected the history entry to survive, got %+v", pruned)
	}
	output := pruned.Outputs[0]
	if output.TrashedPath != "" || !output.Undone {
		t.Fatalf("expected the pruned output to stay undone without a trash path, got %+v", output)
	}
	if !pruned.TrashEmptied || !pruned.Undone {
		t.Fatalf("expected the entry to be marked trashEmptied and undone, got %+v", pruned)
	}

	kept, _ := store.Get("job_new")
	if kept.TrashEmptied || kept.Outputs[0].TrashedPath == "" {
		t.Fatalf("expected the recent trash directory to be kept, got %+v", kept)
	}
	if _, err := os.Stat(kept.Outputs[0].TrashedPath); err != nil {
		t.Fatalf("expected the recent trash file to exist: %v", err)
	}

	reloaded := NewStore(filepath.Join(dir, "fileforge", "job_history.json"))
	reloadedOld, found := reloaded.Get("job_old")
	if !found || !reloadedOld.TrashEmptied || reloadedOld.Outputs[0].TrashedPath != "" {
		t.Fatalf("expected retention state to persist, got %+v", reloadedOld)
	}
}

func TestTrashRetentionPrunesBySize(t *testing.T) {
	store, dir := newTestStore(t)
	oldPath := filepath.Join(dir, "out", "old.pdf")
	newPath := filepath.Join(dir, "out", "new.pdf")
	writeTestFile(t, oldPath, "0123456789")
	writeTestFile(t, newPath, "01234")

	store.RecordJob(terminalJob("job_old", "tool.pdf.merge", models.JobStatusSuccess, oldPath))
	if result := store.Undo("job_old"); !result.Success {
		t.Fatalf("undo old job: %+v", result)
	}
	store.RecordJob(terminalJob("job_new", "tool.pdf.merge", models.JobStatusSuccess, newPath))
	if result := store.Undo("job_new"); !result.Success {
		t.Fatalf("undo new job: %+v", result)
	}

	oldEntry, _ := store.Get("job_old")
	newEntry, _ := store.Get("job_new")
	oldDir := filepath.Dir(oldEntry.Outputs[0].TrashedPath)
	newDir := filepath.Dir(newEntry.Outputs[0].TrashedPath)
	base := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldDir, base, base); err != nil {
		t.Fatalf("backdate old trash directory: %v", err)
	}
	if err := os.Chtimes(newDir, base.Add(time.Hour), base.Add(time.Hour)); err != nil {
		t.Fatalf("backdate new trash directory: %v", err)
	}

	store.SetTrashRetention(5, 0)
	func() {
		store.opMu.Lock()
		defer store.opMu.Unlock()
		if err := store.retainTrash(); err != nil {
			t.Fatalf("run retention: %v", err)
		}
	}()

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("expected the oldest directory to be pruned by size, got %v", err)
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("expected the newest directory to stay within the size limit: %v", err)
	}

	pruned, _ := store.Get("job_old")
	if pruned.Outputs[0].TrashedPath != "" || !pruned.TrashEmptied {
		t.Fatalf("expected the pruned entry to be updated, got %+v", pruned)
	}
	kept, _ := store.Get("job_new")
	if kept.Outputs[0].TrashedPath == "" || kept.TrashEmptied {
		t.Fatalf("expected the kept entry to stay untouched, got %+v", kept)
	}
}

func TestNewStorePrunesExpiredTrashOnLoad(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "fileforge", "job_history.json")
	trashFile := filepath.Join(dir, "fileforge", "trash", "job_old", "0-out.pdf")
	writeTestFile(t, trashFile, "stale")

	entry := models.JobHistoryEntry{
		JobID:  "job_old",
		ToolID: "tool.pdf.merge",
		Undone: true,
		Outputs: []models.JobOutputRecord{{
			Path:        filepath.Join(dir, "out.pdf"),
			SizeBytes:   int64(len("stale")),
			ModifiedAt:  1700000000000,
			TrashedPath: trashFile,
			Undone:      true,
		}},
	}
	payload, err := json.Marshal([]models.JobHistoryEntry{entry})
	if err != nil {
		t.Fatalf("encode history: %v", err)
	}
	writeTestFile(t, historyPath, string(payload))

	expired := time.Now().Add(-defaultTrashMaxAge - 24*time.Hour)
	if err := os.Chtimes(trashFile, expired, expired); err != nil {
		t.Fatalf("backdate trash file: %v", err)
	}
	if err := os.Chtimes(filepath.Dir(trashFile), expired, expired); err != nil {
		t.Fatalf("backdate trash directory: %v", err)
	}

	store := NewStore(historyPath)
	if _, err := os.Stat(filepath.Dir(trashFile)); !os.IsNotExist(err) {
		t.Fatalf("expected NewStore to prune expired trash, got %v", err)
	}
	stored, found := store.Get("job_old")
	if !found {
		t.Fatal("expected the history entry to survive pruning")
	}
	if stored.Outputs[0].TrashedPath != "" || !stored.Outputs[0].Undone || !stored.TrashEmptied {
		t.Fatalf("expected load-time retention to update history, got %+v", stored)
	}
}

func TestTrashStatsCountsNestedRegularFiles(t *testing.T) {
	store, dir := newTestStore(t)
	trashRoot := filepath.Join(dir, "fileforge", trashDirName)

	writeTestFile(t, filepath.Join(trashRoot, "job_1", "0-a.pdf"), "alpha")
	writeTestFile(t, filepath.Join(trashRoot, "job_1", "nested", "1-b.pdf"), "beta-bytes")
	writeTestFile(t, filepath.Join(trashRoot, "stray.txt"), "plain")

	files, bytes, err := store.TrashStats()
	if err != nil {
		t.Fatalf("TrashStats: %v", err)
	}
	if files != 3 {
		t.Fatalf("expected 3 trash files, got %d", files)
	}
	wantBytes := int64(len("alpha") + len("beta-bytes") + len("plain"))
	if bytes != wantBytes {
		t.Fatalf("expected %d trash bytes, got %d", wantBytes, bytes)
	}
}

func TestTrashStatsTreatsMissingOrEmptyTrashAsZero(t *testing.T) {
	store, dir := newTestStore(t)

	files, bytes, err := store.TrashStats()
	if err != nil {
		t.Fatalf("missing trash: %v", err)
	}
	if files != 0 || bytes != 0 {
		t.Fatalf("missing trash: expected 0/0, got %d/%d", files, bytes)
	}

	if err := os.MkdirAll(filepath.Join(dir, "fileforge", trashDirName), 0o755); err != nil {
		t.Fatalf("create empty trash: %v", err)
	}
	files, bytes, err = store.TrashStats()
	if err != nil {
		t.Fatalf("empty trash: %v", err)
	}
	if files != 0 || bytes != 0 {
		t.Fatalf("empty trash: expected 0/0, got %d/%d", files, bytes)
	}
}
