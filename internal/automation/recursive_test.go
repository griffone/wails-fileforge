package automation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

// currentWatcher returns the live watcher of a rule, so tests can assert watch
// bookkeeping without racing the service map.
func currentWatcher(t *testing.T, service *Service, ruleID string) *ruleWatcher {
	t.Helper()

	service.mu.Lock()
	defer service.mu.Unlock()

	watcher := service.watchers[ruleID]
	if watcher == nil {
		t.Fatalf("rule %s has no watcher", ruleID)
	}

	return watcher
}

func TestWatcherRecursiveMatchesNestedFolder(t *testing.T) {
	tests := []struct {
		name      string
		recursive bool
		wantRuns  int
	}{
		{name: "recursion off ignores nested files", recursive: false, wantRuns: 0},
		{name: "recursion on processes nested files", recursive: true, wantRuns: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := newFakeRecipeRunner()
			service, _ := newTestService(t, runner, nil)

			watch := t.TempDir()
			// The nested folders exist before the rule starts: this covers a
			// pre-existing tree, while the adoption test covers new folders.
			nested := filepath.Join(watch, "nested", "deeper")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatalf("create nested dirs: %v", err)
			}

			saveTestRule(t, service, models.AutomationRule{
				Name:      "Nested",
				Enabled:   true,
				WatchDir:  watch,
				RecipeID:  testRecipeID,
				Recursive: test.recursive,
			})
			startService(t, service)

			writeTestFile(t, filepath.Join(nested, "note.txt"), "nested file")

			if test.wantRuns == 0 {
				assertRunStaysAt(t, runner, 0, 300*time.Millisecond)

				return
			}
			waitForRunCount(t, runner, test.wantRuns, 3*time.Second)
		})
	}
}

func TestWatcherRecursiveAdoptsNewSubdirectory(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRule{
		Name:      "Nested",
		Enabled:   true,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		Recursive: true,
	})
	startService(t, service)

	late := filepath.Join(watch, "late")
	if err := os.Mkdir(late, 0o755); err != nil {
		t.Fatalf("create late dir: %v", err)
	}

	waitFor(t, 3*time.Second, "the created subdirectory to be watched", func() bool {
		return currentWatcher(t, service, rule.ID).watchesDirectory(late)
	})

	writeTestFile(t, filepath.Join(late, "note.txt"), "arrived later")
	waitForRunCount(t, runner, 1, 3*time.Second)
}

// TestWatcherRecursiveAdoptsMovedInDirectory proves a folder renamed into the
// watch tree keeps the files it already contains: watching alone would miss
// them because no create event is emitted for the children.
func TestWatcherRecursiveAdoptsMovedInDirectory(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	saveTestRule(t, service, models.AutomationRule{
		Name:      "Nested",
		Enabled:   true,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		Recursive: true,
	})
	startService(t, service)

	staging := t.TempDir()
	writeTestFile(t, filepath.Join(staging, "moved.txt"), "moved with the folder")
	if err := os.Rename(staging, filepath.Join(watch, "incoming")); err != nil {
		t.Fatalf("move directory into the watch folder: %v", err)
	}

	waitForRunCount(t, runner, 1, 3*time.Second)
}

func TestWatcherPatternsFilterByBaseName(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	saveTestRule(t, service, models.AutomationRule{
		Name:       "Reports",
		Enabled:    true,
		WatchDir:   watch,
		RecipeID:   testRecipeID,
		Extensions: []string{"txt"},
		Patterns:   []string{"report-*"},
	})
	startService(t, service)

	// Accepted extension, excluded name.
	writeTestFile(t, filepath.Join(watch, "note.txt"), "not a report")
	assertRunStaysAt(t, runner, 0, 250*time.Millisecond)

	// Matching name and accepted extension.
	writeTestFile(t, filepath.Join(watch, "report-2026.txt"), "report")
	waitForRunCount(t, runner, 1, 3*time.Second)

	// Matching pattern, rejected extension: both filters must pass.
	writeTestFile(t, filepath.Join(watch, "report-2026.png"), "wrong kind")
	assertRunStaysAt(t, runner, 1, 300*time.Millisecond)
}

func TestWatcherPatternsMatchNestedBaseName(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	nested := filepath.Join(watch, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	saveTestRule(t, service, models.AutomationRule{
		Name:      "Reports",
		Enabled:   true,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		Recursive: true,
		Patterns:  []string{"report-*"},
	})
	startService(t, service)

	writeTestFile(t, filepath.Join(nested, "note.txt"), "not a report")
	assertRunStaysAt(t, runner, 0, 250*time.Millisecond)

	writeTestFile(t, filepath.Join(nested, "report-nested.txt"), "report")
	waitForRunCount(t, runner, 1, 3*time.Second)
}

func TestSaveRuleRejectsInvalidPatterns(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)
	watch := t.TempDir()

	tests := []struct {
		name     string
		patterns []string
		wantText string
	}{
		{name: "unbalanced bracket", patterns: []string{"[abc"}, wantText: "is not valid"},
		{name: "folder separator", patterns: []string{"nested/*.txt"}, wantText: "file name only"},
		{name: "double star", patterns: []string{"**.txt"}, wantText: "file name only"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := service.SaveRule(models.AutomationRule{
				Name:     "Reports",
				WatchDir: watch,
				RecipeID: testRecipeID,
				Patterns: test.patterns,
			})

			if response.Success {
				t.Fatalf("expected invalid patterns to be rejected, got %+v", response.Rule)
			}
			if response.Error == nil || response.Error.DetailCode != "AUTOMATION_RULE_PATTERN_INVALID" {
				t.Fatalf("unexpected error: %+v", response.Error)
			}
			if !strings.Contains(response.Message, test.wantText) {
				t.Fatalf("message %q does not mention %q", response.Message, test.wantText)
			}
		})
	}
}

func TestSaveRuleNormalizesPatterns(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	saved := saveTestRule(t, service, models.AutomationRule{
		Name:     "Reports",
		WatchDir: t.TempDir(),
		RecipeID: testRecipeID,
		Patterns: []string{"  report-*.txt ", "report-*.txt", "", "*.pdf"},
	})

	want := []string{"report-*.txt", "*.pdf"}
	if len(saved.Patterns) != len(want) {
		t.Fatalf("patterns = %+v, want %+v", saved.Patterns, want)
	}
	for index := range want {
		if saved.Patterns[index] != want[index] {
			t.Fatalf("patterns = %+v, want %+v", saved.Patterns, want)
		}
	}
}

func TestWatcherRecursiveIgnoresOutputSubfolder(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	output := filepath.Join(watch, "converted")
	if err := os.MkdirAll(output, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	saveTestRule(t, service, models.AutomationRule{
		Name:      "Inbox",
		Enabled:   true,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		Recursive: true,
		OutputDir: output,
	})
	startService(t, service)

	writeTestFile(t, filepath.Join(watch, "note.txt"), "recursive loop guard")
	waitForRunCount(t, runner, 1, 3*time.Second)
	waitFor(t, 3*time.Second, "the output inside the watch tree", func() bool {
		_, err := os.Stat(filepath.Join(output, "note-out.txt"))

		return err == nil
	})

	// A file planted directly in the output folder must not start a run even
	// though the whole tree is watched recursively.
	writeTestFile(t, filepath.Join(output, "planted.txt"), "must stay ignored")
	assertRunStaysAt(t, runner, 1, 400*time.Millisecond)
}

func TestWatcherRecursiveCapsWatchedDirectories(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	const extraDirs = maxWatchedDirectories + 44
	for index := 0; index < extraDirs; index++ {
		dir := filepath.Join(watch, fmt.Sprintf("dir-%03d", index))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}

	rule := saveTestRule(t, service, models.AutomationRule{
		Name:      "Huge tree",
		Enabled:   true,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		Recursive: true,
	})
	startService(t, service)

	watcher := currentWatcher(t, service, rule.ID)
	if got := watcher.watchedDirectoryCount(); got != maxWatchedDirectories {
		t.Fatalf("watched directories = %d, want the cap %d", got, maxWatchedDirectories)
	}
	if !watcher.directoryCapReached() {
		t.Fatal("expected the directory cap to be reported")
	}

	// The rule keeps working for the folders inside the cap.
	writeTestFile(t, filepath.Join(watch, "dir-000", "note.txt"), "inside the cap")
	waitForRunCount(t, runner, 1, 3*time.Second)
	assertRunStaysAt(t, runner, 1, 200*time.Millisecond)
}
