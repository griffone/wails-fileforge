package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
)

// organizeTestRule builds a minimal organize rule with the shared fixture
// directory and the txt extension the fake recipe runner also accepts.
func organizeTestRule(watch, target string) models.AutomationRule {
	return models.AutomationRule{
		Name:            "Organize",
		WatchDir:        watch,
		Action:          models.AutomationActionOrganize,
		TargetDir:       target,
		CollisionPolicy: models.AutomationCollisionSkip,
		Extensions:      []string{"txt"},
	}
}

// planOncePreview saves a rule, previews it and returns the stored rule and the
// preview response. It is the normal UI order: save, preview, enable.
func planOncePreview(t *testing.T, service *Service, rule models.AutomationRule) (models.AutomationRule, models.AutomationOrganizePreviewResponse) {
	t.Helper()

	saved := saveTestRule(t, service, rule)
	response := service.PreviewOrganize(saved)
	if !response.Success {
		t.Fatalf("PreviewOrganize failed: %s (%+v)", response.Message, response.Error)
	}
	stored, found := service.store.Get(saved.ID)
	if !found {
		t.Fatal("saved rule disappeared from the store")
	}

	return stored, response
}

// runOrganizeNow executes one file through the watcher path without fsnotify,
// using the same entry point a stable event uses.
func runOrganizeNow(t *testing.T, service *Service, rule models.AutomationRule, path string) {
	t.Helper()

	watcher, err := newRuleWatcher(service, rule, context.Background())
	if err != nil {
		t.Fatalf("create organizer watcher: %v", err)
	}
	t.Cleanup(func() {
		watcher.stop(time.Now().Add(time.Second))
	})

	fingerprint, err := fingerprintFile(path)
	if err != nil {
		t.Fatalf("fingerprint %s: %v", path, err)
	}
	watcher.runOrganize(queuedPath{path: path, fingerprint: fingerprint, hasFingerprint: true})
}

func lastRun(t *testing.T, store *Store, ruleID string) models.AutomationRule {
	t.Helper()

	rule, found := store.Get(ruleID)
	if !found {
		t.Fatalf("rule %s is missing from the store", ruleID)
	}
	if rule.LastRunAt == 0 {
		t.Fatalf("rule %s has no recorded run", ruleID)
	}

	return rule
}

// treeSnapshot captures every path under root with its size, so a preview can
// be proven to write nothing.
func treeSnapshot(t *testing.T, root string) map[string]int64 {
	t.Helper()

	snapshot := map[string]int64{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			snapshot[path] = -1

			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		snapshot[path] = info.Size()

		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}

	return snapshot
}

func assertTreeUnchanged(t *testing.T, root string, before map[string]int64) {
	t.Helper()

	after := treeSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("%s changed: had %d entries, now %d", root, len(before), len(after))
	}
	for path, size := range before {
		if after[path] != size {
			t.Fatalf("%s changed or disappeared (was %d bytes, now %d)", path, size, after[path])
		}
	}
}

func TestOrganizeTemplateRendersEveryToken(t *testing.T) {
	target := t.TempDir()
	modified := time.Date(2024, 3, 9, 14, 30, 0, 0, time.UTC)

	meta := organizeMetaFor(filepath.Join("watch", "report.pdf"), modified, "Quarterly Report")

	cases := []struct {
		template string
		want     string
	}{
		{template: "{name}", want: "report"},
		{template: "{ext}", want: ".pdf"},
		{template: "{year}", want: "2024"},
		{template: "{month}", want: "03"},
		{template: "{date}", want: "2024-03-09"},
		{template: "{title}", want: "Quarterly Report"},
		{template: "{name}{ext}", want: "report.pdf"},
		{template: "{title} {date}{ext}", want: "Quarterly Report 2024-03-09.pdf"},
	}

	for _, test := range cases {
		t.Run(test.template, func(t *testing.T) {
			parts, err := parseOrganizeTemplate(test.template)
			if err != nil {
				t.Fatalf("parse %q: %v", test.template, err)
			}
			rendered, err := renderOrganizeTemplate(parts, meta)
			if err != nil {
				t.Fatalf("render %q: %v", test.template, err)
			}
			if rendered != test.want {
				t.Fatalf("render %q = %q, want %q", test.template, rendered, test.want)
			}
		})
	}

	// Blank template keeps the original base name, folders stay optional.
	destination, err := renderOrganizeDestination(target, "{year}/{month}", "{title}{ext}", meta)
	if err != nil {
		t.Fatalf("render destination: %v", err)
	}
	want := filepath.Join(target, "2024", "03", "Quarterly Report.pdf")
	if destination != want {
		t.Fatalf("destination = %q, want %q", destination, want)
	}

	noTemplate, err := renderOrganizeDestination(target, "", "", meta)
	if err != nil {
		t.Fatalf("render destination without templates: %v", err)
	}
	if noTemplate != filepath.Join(target, "report.pdf") {
		t.Fatalf("blank templates must keep the base name, got %q", noTemplate)
	}
}

// TestOrganizeTitleFallsBackToName proves {title} falls back to {name} when no
// metadata title is available, and that the real seam is wired to the shared
// suggestion engine.
func TestOrganizeTitleFallsBackToName(t *testing.T) {
	service, _ := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()

	path := filepath.Join(watch, "Report 2026.pdf")
	if err := os.WriteFile(path, []byte("not a real pdf"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// The default seam is the shared suggestion engine; a plain text file with
	// no metadata still yields a usable name.
	plain := filepath.Join(watch, "Report 2026.txt")
	if err := os.WriteFile(plain, []byte("text"), 0o644); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}
	if title := defaultOrganizeTitle(plain); title == "" {
		t.Fatal("the default title seam must resolve a name for a plain file")
	}

	// No title available: {title} must keep the base name.
	service.titleFor = func(string) string { return "" }
	meta := organizeMetaFor(path, time.Now(), service.organizeTitle(path))
	destination, err := renderOrganizeDestination(target, "", "{title}{ext}", meta)
	if err != nil {
		t.Fatalf("render destination: %v", err)
	}
	if destination != filepath.Join(target, "Report 2026.pdf") {
		t.Fatalf("{title} must fall back to {name}, got %q", destination)
	}

	// A metadata title is used when present.
	meta = organizeMetaFor(path, time.Now(), "Quarterly Report")
	destination, err = renderOrganizeDestination(target, "", "{title}{ext}", meta)
	if err != nil {
		t.Fatalf("render destination: %v", err)
	}
	if destination != filepath.Join(target, "Quarterly Report.pdf") {
		t.Fatalf("{title} must use the metadata title, got %q", destination)
	}
}

func TestOrganizeSaveRejectsUnknownTemplateToken(t *testing.T) {
	service, _ := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()

	rule := organizeTestRule(watch, target)
	rule.NameTemplate = "{bogus}{ext}"
	response := service.SaveRule(rule)
	if response.Success {
		t.Fatal("a rule with an unknown token must not be saved")
	}
	if !strings.Contains(response.Message, "bogus") {
		t.Fatalf("the error must name the unknown token, got %q", response.Message)
	}
	if response.Error == nil || response.Error.DetailCode != "AUTOMATION_RULE_TEMPLATE_INVALID" {
		t.Fatalf("unexpected error: %+v", response.Error)
	}

	rule = organizeTestRule(watch, target)
	rule.FolderTemplate = "{year}/{unknown}"
	if response := service.SaveRule(rule); response.Success {
		t.Fatal("an unknown folder token must not be saved")
	}
}

func TestOrganizeSaveRejectsTargetInsideWatchFolder(t *testing.T) {
	service, _ := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	inside := filepath.Join(watch, "organized")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatalf("create nested target: %v", err)
	}
	outside := t.TempDir()

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "target equal to watch folder", target: watch, want: false},
		{name: "target inside watch folder", target: inside, want: false},
		{name: "target outside watch folder", target: outside, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := service.SaveRule(organizeTestRule(watch, test.target))
			if response.Success != test.want {
				t.Fatalf("Success = %t, want %t (message: %s)", response.Success, test.want, response.Message)
			}
			if !test.want {
				if !strings.Contains(response.Message, "cannot") {
					t.Fatalf("expected a clear message, got %q", response.Message)
				}
				if response.Error == nil || response.Error.DetailCode != "AUTOMATION_RULE_TARGET_INSIDE_WATCH" {
					t.Fatalf("unexpected error: %+v", response.Error)
				}
			}
		})
	}
}

// TestOrganizePreviewMatchesExecution proves the resolved collision path shown
// by the preview is exactly the path execution uses, including the -2 suffix
// for two source files that render the same destination.
func TestOrganizePreviewMatchesExecution(t *testing.T) {
	service, store := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()

	for _, sub := range []string{"a", "b"} {
		dir := filepath.Join(watch, sub)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("create source folder: %v", err)
		}
		writeTestFile(t, filepath.Join(dir, "report.txt"), "from "+sub)
	}

	rule := organizeTestRule(watch, target)
	rule.Recursive = true
	rule.CollisionPolicy = models.AutomationCollisionSuffix
	rule.Extensions = nil

	stored, preview := planOncePreview(t, service, rule)
	if len(preview.Moves) != 2 {
		t.Fatalf("preview returned %d moves, want 2: %+v", len(preview.Moves), preview.Moves)
	}
	planned := map[string]string{}
	for _, move := range preview.Moves {
		if move.Action != models.OrganizeMoveAction {
			t.Fatalf("expected every item to be planned, got %+v", move)
		}
		planned[move.Source] = move.Destination
	}

	wantFirst := filepath.Join(target, "report.txt")
	wantSecond := filepath.Join(target, "report-2.txt")
	if planned[filepath.Join(watch, "a", "report.txt")] != wantFirst {
		t.Fatalf("first destination = %q, want %q", planned[filepath.Join(watch, "a", "report.txt")], wantFirst)
	}
	if planned[filepath.Join(watch, "b", "report.txt")] != wantSecond {
		t.Fatalf("second destination = %q, want %q", planned[filepath.Join(watch, "b", "report.txt")], wantSecond)
	}

	// Execute in the same deterministic order and check the exact previewed
	// paths, not merely "some moved file".
	sources := make([]string, 0, len(planned))
	for source := range planned {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	for _, source := range sources {
		runOrganizeNow(t, service, stored, source)
	}

	for source, destination := range planned {
		if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("source %s was not moved", source)
		}
		content, err := os.ReadFile(destination)
		if err != nil {
			t.Fatalf("destination %s missing: %v", destination, err)
		}
		if !strings.HasPrefix(string(content), "from ") {
			t.Fatalf("unexpected destination content %q", content)
		}
	}
	if storedRun := lastRun(t, store, stored.ID); storedRun.LastStatus != models.JobStatusSuccess {
		t.Fatalf("unexpected last run: %+v", storedRun)
	}
}

// TestOrganizeCollisionSkipLeavesFileInPlace proves the skip policy neither
// overwrites the existing file nor moves the source, and that the preview says
// so before execution.
func TestOrganizeCollisionSkipLeavesFileInPlace(t *testing.T) {
	service, store := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()

	source := filepath.Join(watch, "report.txt")
	writeTestFile(t, source, "new version")
	existing := filepath.Join(target, "report.txt")
	writeTestFile(t, existing, "existing version")

	rule := organizeTestRule(watch, target)
	stored, preview := planOncePreview(t, service, rule)
	if len(preview.Moves) != 1 {
		t.Fatalf("preview returned %d moves, want 1", len(preview.Moves))
	}
	if preview.Moves[0].Action != models.OrganizeSkipAction {
		t.Fatalf("expected a skip plan, got %+v", preview.Moves[0])
	}
	if preview.Moves[0].Reason == "" {
		t.Fatal("a skip must carry a reason")
	}

	runOrganizeNow(t, service, stored, source)

	content, err := os.ReadFile(existing)
	if err != nil || string(content) != "existing version" {
		t.Fatalf("the existing destination was overwritten: %q (%v)", content, err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("the source must stay in place on skip: %v", err)
	}
	run := lastRun(t, store, stored.ID)
	if run.LastStatus != models.JobStatusPartialSuccess || !strings.Contains(run.LastMessage, "already exists") {
		t.Fatalf("unexpected skip run: %+v", run)
	}
}

// TestOrganizeCrossVolumeVerificationFailure proves a failed verification
// leaves the source untouched, removes the partial copy and reports failure.
func TestOrganizeCrossVolumeVerificationFailure(t *testing.T) {
	service, store := newTestService(t, newFakeRecipeRunner(), nil)
	history := jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history.json"))
	service.SetMoveHistory(history)

	watch := t.TempDir()
	target := t.TempDir()
	source := filepath.Join(watch, "report.txt")
	writeTestFile(t, source, "must survive")

	rule := organizeTestRule(watch, target)
	stored, _ := planOncePreview(t, service, rule)

	service.mover = organizeMover{
		rename:     func(_, _ string) error { return errors.New("cross-device link") },
		copyFile:   copyOrganizeFile,
		verifyCopy: func(_, _ string) error { return errors.New("content hash differs") },
		remove:     os.Remove,
		crossVolume: func(_, _ string) bool {
			return true
		},
	}

	runOrganizeNow(t, service, stored, source)

	if _, err := os.Stat(source); err != nil {
		t.Fatalf("the source must be intact after a failed verification: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "report.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the unverified copy must be removed, got err=%v", err)
	}
	run := lastRun(t, store, stored.ID)
	if run.LastStatus != models.JobStatusFailed {
		t.Fatalf("expected a failed run, got %+v", run)
	}
	if !strings.Contains(run.LastMessage, "verified") {
		t.Fatalf("the failure must explain the verification, got %q", run.LastMessage)
	}
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("a failed move must not be recorded in history: %+v", entries)
	}
}

// TestOrganizeCrossVolumeCopySucceeds exercises the copy, verify, remove path
// that a real cross-volume move uses.
func TestOrganizeCrossVolumeCopySucceeds(t *testing.T) {
	service, store := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()
	source := filepath.Join(watch, "report.txt")
	writeTestFile(t, source, "copied across volumes")

	rule := organizeTestRule(watch, target)
	stored, _ := planOncePreview(t, service, rule)

	service.mover = organizeMover{
		rename:      func(_, _ string) error { return errors.New("cross-device link") },
		copyFile:    copyOrganizeFile,
		verifyCopy:  verifyOrganizeCopy,
		remove:      os.Remove,
		crossVolume: func(_, _ string) bool { return true },
	}

	runOrganizeNow(t, service, stored, source)

	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the source must be removed after a verified copy, got err=%v", err)
	}
	content, err := os.ReadFile(filepath.Join(target, "report.txt"))
	if err != nil || string(content) != "copied across volumes" {
		t.Fatalf("unexpected destination content %q (%v)", content, err)
	}
	if run := lastRun(t, store, stored.ID); run.LastStatus != models.JobStatusSuccess {
		t.Fatalf("expected success, got %+v", run)
	}
}

// TestOrganizeSkipsFileChangedAfterStabilityWindow proves the frozen
// fingerprint from the stability window is rechecked before the move.
func TestOrganizeSkipsFileChangedAfterStabilityWindow(t *testing.T) {
	service, store := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()
	source := filepath.Join(watch, "report.txt")
	writeTestFile(t, source, "short")

	rule := organizeTestRule(watch, target)
	stored, _ := planOncePreview(t, service, rule)

	stabilityFingerprint, err := fingerprintFile(source)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	// The file keeps growing after the stability window closed.
	writeTestFile(t, source, "short plus a lot more content")

	watcher, err := newRuleWatcher(service, stored, context.Background())
	if err != nil {
		t.Fatalf("create organizer watcher: %v", err)
	}
	t.Cleanup(func() { watcher.stop(time.Now().Add(time.Second)) })
	watcher.runOrganize(queuedPath{path: source, fingerprint: stabilityFingerprint, hasFingerprint: true})

	if _, err := os.Stat(source); err != nil {
		t.Fatalf("a changed file must stay in the watch folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "report.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a changed file must not be moved, got err=%v", err)
	}
	run := lastRun(t, store, stored.ID)
	if run.LastStatus != models.JobStatusPartialSuccess || !strings.Contains(run.LastMessage, "stability window") {
		t.Fatalf("unexpected run for a changed file: %+v", run)
	}
}

// TestOrganizePreviewWritesNothingAndRecordsNothing proves the dry run is
// really dry: no file appears, no file changes and no history entry exists.
func TestOrganizePreviewWritesNothingAndRecordsNothing(t *testing.T) {
	service, _ := newTestService(t, newFakeRecipeRunner(), nil)
	history := jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history.json"))
	service.SetMoveHistory(history)
	service.SetRunStore(NewRunStore(filepath.Join(t.TempDir(), "runs.json")))

	watch := t.TempDir()
	target := t.TempDir()
	writeTestFile(t, filepath.Join(watch, "one.txt"), "one")
	writeTestFile(t, filepath.Join(watch, "two.txt"), "two")
	sub := filepath.Join(watch, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("create nested folder: %v", err)
	}
	writeTestFile(t, filepath.Join(sub, "three.txt"), "three")

	watchBefore := treeSnapshot(t, watch)
	targetBefore := treeSnapshot(t, target)

	rule := organizeTestRule(watch, target)
	rule.FolderTemplate = "{year}/{month}"
	rule.NameTemplate = "{name}-{date}{ext}"
	stored, preview := planOncePreview(t, service, rule)

	if len(preview.Moves) != 2 {
		t.Fatalf("preview returned %d moves, want 2 (nested file must be ignored without recursion): %+v", len(preview.Moves), preview.Moves)
	}
	for _, move := range preview.Moves {
		if move.Action != models.OrganizeMoveAction {
			t.Fatalf("unexpected planned item: %+v", move)
		}
		if !strings.HasPrefix(move.Destination, target+string(filepath.Separator)) {
			t.Fatalf("destination outside target: %q", move.Destination)
		}
	}

	assertTreeUnchanged(t, watch, watchBefore)
	assertTreeUnchanged(t, target, targetBefore)
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("preview must not record history, got %+v", entries)
	}
	if runs := service.ListRuns(0); len(runs.Runs) != 0 {
		t.Fatalf("preview must not record runs, got %+v", runs.Runs)
	}
	if !preview.PreviewStored {
		t.Fatal("a preview of the saved configuration must store its hash")
	}
	if stored.PreviewHash != preview.PreviewHash || stored.PreviewHash == "" {
		t.Fatalf("preview hash was not persisted: %+v", stored)
	}
}

func TestOrganizeEnableRequiresMatchingPreview(t *testing.T) {
	service, store := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()
	writeTestFile(t, filepath.Join(watch, "report.txt"), "content")

	rule := organizeTestRule(watch, target)
	saved := saveTestRule(t, service, rule)

	// No preview yet: enabling is refused by the service, not only by the UI.
	response := service.SetRuleEnabled(saved.ID, true)
	if response.Success {
		t.Fatal("enabling without a preview must be refused")
	}
	if response.Error == nil || response.Error.DetailCode != "AUTOMATION_RULE_PREVIEW_REQUIRED" {
		t.Fatalf("unexpected enable error: %+v", response.Error)
	}

	preview := service.PreviewOrganize(saved)
	if !preview.Success || !preview.PreviewStored {
		t.Fatalf("preview failed or was not stored: %s", preview.Message)
	}

	response = service.SetRuleEnabled(saved.ID, true)
	if !response.Success || response.Rule == nil || !response.Rule.Enabled {
		t.Fatalf("enabling after a matching preview failed: %s", response.Message)
	}

	// Changing a relevant field invalidates the stored preview.
	edited, found := store.Get(saved.ID)
	if !found {
		t.Fatal("rule missing from the store")
	}
	edited.NameTemplate = "{title}{ext}"
	saveResponse := service.SaveRule(edited)
	if !saveResponse.Success || saveResponse.Rule == nil {
		t.Fatalf("saving the edited rule failed: %s", saveResponse.Message)
	}
	if saveResponse.Rule.Enabled {
		t.Fatal("a saved rule with an invalidated preview must be disabled")
	}
	if saveResponse.Rule.PreviewHash != "" {
		t.Fatalf("the stale preview hash must be cleared: %+v", saveResponse.Rule)
	}

	if response := service.SetRuleEnabled(saved.ID, true); response.Success {
		t.Fatal("enabling after a configuration change must be refused until previewed again")
	}

	repreview := service.PreviewOrganize(*saveResponse.Rule)
	if !repreview.Success || !repreview.PreviewStored {
		t.Fatalf("re-preview failed: %s", repreview.Message)
	}
	if response := service.SetRuleEnabled(saved.ID, true); !response.Success {
		t.Fatalf("enabling after re-preview failed: %s", response.Message)
	}
}

// TestOrganizeUndoRestoresMovedFile proves a moved file is recorded in the
// shared history and that the existing Undo/Restore path brings it back to its
// original watch-folder path.
func TestOrganizeUndoRestoresMovedFile(t *testing.T) {
	service, _ := newTestService(t, newFakeRecipeRunner(), nil)
	historyPath := filepath.Join(t.TempDir(), "history", "job_history.json")
	history := jobhistory.NewStore(historyPath)
	service.SetMoveHistory(history)

	watch := t.TempDir()
	target := t.TempDir()
	source := filepath.Join(watch, "report.txt")
	writeTestFile(t, source, "undo me")

	rule := organizeTestRule(watch, target)
	rule.FolderTemplate = "{year}"
	stored, _ := planOncePreview(t, service, rule)

	runOrganizeNow(t, service, stored, source)

	entries := history.List()
	if len(entries) != 1 {
		t.Fatalf("expected one history entry, got %+v", entries)
	}
	entry := entries[0]
	if len(entry.Outputs) != 1 {
		t.Fatalf("expected one recorded move, got %+v", entry.Outputs)
	}
	destination := entry.Outputs[0].Path
	if !strings.HasPrefix(destination, target+string(filepath.Separator)) {
		t.Fatalf("recorded path is not the destination: %q", destination)
	}
	if entry.Outputs[0].RestorePath != source {
		t.Fatalf("recorded restore path = %q, want %q", entry.Outputs[0].RestorePath, source)
	}

	undo := history.Undo(entry.JobID)
	if !undo.Success || len(undo.Moved) != 1 {
		t.Fatalf("undo failed: %+v", undo)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("undo must move the destination to the trash, got err=%v", err)
	}

	restore := history.Restore(entry.JobID)
	if !restore.Success || len(restore.Moved) != 1 {
		t.Fatalf("restore failed: %+v", restore)
	}
	if restore.Moved[0] != source {
		t.Fatalf("restore returned %q, want the original source %q", restore.Moved[0], source)
	}
	content, err := os.ReadFile(source)
	if err != nil || string(content) != "undo me" {
		t.Fatalf("the file was not restored to its source: %q (%v)", content, err)
	}
}

// TestOrganizeRecursiveAndPatternFilters proves organize rules honor the same
// recursion and name filters as recipe rules.
func TestOrganizeRecursiveAndPatternFilters(t *testing.T) {
	service, _ := newTestService(t, newFakeRecipeRunner(), nil)
	watch := t.TempDir()
	target := t.TempDir()

	writeTestFile(t, filepath.Join(watch, "invoice-2026.txt"), "match root")
	writeTestFile(t, filepath.Join(watch, "notes.txt"), "no match root")
	sub := filepath.Join(watch, "archive")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("create subfolder: %v", err)
	}
	writeTestFile(t, filepath.Join(sub, "invoice-2025.txt"), "match nested")
	writeTestFile(t, filepath.Join(sub, "notes-2025.txt"), "no match nested")

	rule := organizeTestRule(watch, target)
	rule.Patterns = []string{"invoice-*.txt"}

	_, shallow := planOncePreview(t, service, rule)
	if len(shallow.Moves) != 1 || shallow.Moves[0].Source != filepath.Join(watch, "invoice-2026.txt") {
		t.Fatalf("unexpected non-recursive plan: %+v", shallow.Moves)
	}

	rule.Recursive = true
	_, deep := planOncePreview(t, service, rule)
	sources := make([]string, 0, len(deep.Moves))
	for _, move := range deep.Moves {
		sources = append(sources, move.Source)
	}
	sort.Strings(sources)
	want := []string{filepath.Join(watch, "invoice-2026.txt"), filepath.Join(sub, "invoice-2025.txt")}
	sort.Strings(want)
	if fmt.Sprint(sources) != fmt.Sprint(want) {
		t.Fatalf("recursive pattern plan = %v, want %v", sources, want)
	}
	// Both nested and root files land at the target root; the second one gets
	// a suffix because the base names differ but the rendered names collide
	// only when identical. Different names here, so both keep their names.
	if len(deep.Moves) != 2 {
		t.Fatalf("unexpected recursive plan: %+v", deep.Moves)
	}
}

// TestLegacyRuleStillRunsAsRecipe proves a rule written before the action field
// existed loads as a recipe rule and processes a file through the watcher.
func TestLegacyRuleStillRunsAsRecipe(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, store := newTestService(t, runner, nil)

	watch := t.TempDir()
	legacy := models.AutomationRule{
		ID:         "rule_legacy",
		Name:       "Legacy",
		Enabled:    true,
		WatchDir:   watch,
		RecipeID:   testRecipeID,
		Extensions: []string{"txt"},
		CreatedAt:  1,
	}
	// An empty action is omitted by the JSON encoder, which is exactly the
	// shape written by the version before organize existed.
	payload, err := json.Marshal([]models.AutomationRule{legacy})
	if err != nil {
		t.Fatalf("encode legacy rule: %v", err)
	}
	if err := os.WriteFile(store.path, payload, 0o644); err != nil {
		t.Fatalf("write legacy store: %v", err)
	}
	store.load()

	loaded, found := store.Get("rule_legacy")
	if !found {
		t.Fatal("legacy rule was not loaded")
	}
	if loaded.Action != models.AutomationActionRecipe {
		t.Fatalf("legacy rule loaded with action %q, want %q", loaded.Action, models.AutomationActionRecipe)
	}

	startService(t, service)
	writeTestFile(t, filepath.Join(watch, "incoming.txt"), "legacy rule input")

	waitForRunCount(t, runner, 1, 5*time.Second)
}
