package automation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

func TestWatcherRunsRecipeOnceForNewFile(t *testing.T) {
	runner := newFakeRecipeRunner()
	events := make(chan models.AutomationRunEventV1, 16)
	service, store := newTestService(t, runner, func(event models.AutomationRunEventV1) {
		events <- event
	})

	watch := t.TempDir()
	output := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRuleV1{
		Name:      "Inbox",
		Enabled:   true,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		OutputDir: output,
		Options:   map[string]any{"quality": float64(80)},
	})

	startService(t, service)

	source := filepath.Join(watch, "note.txt")
	writeTestFile(t, source, "hello automation")

	var request models.RecipeRequestV1
	select {
	case request = <-runner.runCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the recipe run request")
	}

	if request.RecipeID != testRecipeID {
		t.Fatalf("unexpected recipe id: %q", request.RecipeID)
	}
	if len(request.InputPaths) != 1 || request.InputPaths[0] != source {
		t.Fatalf("unexpected input paths: %+v", request.InputPaths)
	}
	if request.OutputDir != output {
		t.Fatalf("unexpected output dir: %q", request.OutputDir)
	}
	if request.Options["quality"] != float64(80) {
		t.Fatalf("options were not passed through: %+v", request.Options)
	}

	emitted := waitForEvents(t, events, 3*time.Second)
	if len(emitted) != 2 {
		t.Fatalf("expected a running and a terminal event, got %+v", emitted)
	}
	if emitted[0].Status != models.JobStatusRunning {
		t.Fatalf("first event should be running, got %+v", emitted[0])
	}
	if emitted[1].Status != models.JobStatusSuccess {
		t.Fatalf("terminal event should be success, got %+v", emitted[1])
	}
	for _, event := range emitted {
		if event.RuleID != rule.ID || event.RuleName != rule.Name || event.FilePath != source {
			t.Fatalf("unexpected event identity: %+v", event)
		}
		if event.At <= 0 {
			t.Fatalf("event is missing a timestamp: %+v", event)
		}
	}

	expectedOutputs := []string{filepath.Join(output, "note-out.txt")}
	if !reflect.DeepEqual(emitted[1].Outputs, expectedOutputs) {
		t.Fatalf("unexpected terminal outputs: %+v", emitted[1].Outputs)
	}

	waitFor(t, 3*time.Second, "the last run to be persisted", func() bool {
		stored, found := store.Get(rule.ID)

		return found && stored.LastStatus == models.JobStatusSuccess
	})
	stored, found := store.Get(rule.ID)
	if !found {
		t.Fatal("rule disappeared from the store")
	}
	if stored.LastFile != source || stored.LastRunAt <= 0 {
		t.Fatalf("unexpected persisted last run: %+v", stored)
	}
	if !reflect.DeepEqual(stored.LastOutputs, expectedOutputs) {
		t.Fatalf("unexpected persisted outputs: %+v", stored.LastOutputs)
	}
	if stored.RuntimeStatus != "" || stored.RuntimeReason != "" {
		t.Fatalf("runtime fields must not be persisted: %+v", stored)
	}
	assertRunStaysAt(t, runner, 1, 200*time.Millisecond)
}

func TestWatcherDebouncesPartialWrites(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	path := filepath.Join(watch, "chunked.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	for index := 0; index < 5; index++ {
		if _, err := file.WriteString("chunk-"); err != nil {
			t.Fatalf("write chunk: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	waitForRunCount(t, runner, 1, 3*time.Second)
	assertRunStaysAt(t, runner, 1, 300*time.Millisecond)
}

func TestWatcherIgnoresUnsupportedAndTemporaryFiles(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	for _, name := range []string{"photo.png", ".hidden.txt", "note.txt.part", "~$lock.txt", "backup.txt~"} {
		writeTestFile(t, filepath.Join(watch, name), "ignored")
	}
	assertRunStaysAt(t, runner, 0, 250*time.Millisecond)

	writeTestFile(t, filepath.Join(watch, "real.txt"), "process me")
	waitForRunCount(t, runner, 1, 3*time.Second)
}

func TestWatcherDoesNotRunOnItsOwnOutput(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	source := filepath.Join(watch, "loop.txt")
	writeTestFile(t, source, "loop prevention")

	waitForRunCount(t, runner, 1, 3*time.Second)

	output := filepath.Join(watch, "loop-out.txt")
	waitFor(t, 3*time.Second, "the generated output", func() bool {
		_, err := os.Stat(output)

		return err == nil
	})
	assertRunStaysAt(t, runner, 1, 400*time.Millisecond)
}

func TestWatcherIgnoresOutputSubfolder(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	output := filepath.Join(watch, "converted")
	if err := os.Mkdir(output, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	saveTestRule(t, service, models.AutomationRuleV1{
		Name:      "Inbox",
		Enabled:   true,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		OutputDir: output,
	})
	startService(t, service)

	writeTestFile(t, filepath.Join(watch, "note.txt"), "output inside the watch folder")

	waitForRunCount(t, runner, 1, 3*time.Second)
	waitFor(t, 3*time.Second, "the output file", func() bool {
		_, err := os.Stat(filepath.Join(output, "note-out.txt"))

		return err == nil
	})
	assertRunStaysAt(t, runner, 1, 400*time.Millisecond)
}

func TestDeleteSourceOnlyOnUnchangedSuccess(t *testing.T) {
	tests := []struct {
		name         string
		deleteSource bool
		terminal     string
		wantSource   bool
	}{
		{name: "default keeps the source", deleteSource: false, terminal: models.JobStatusSuccess, wantSource: true},
		{name: "success deletes the source", deleteSource: true, terminal: models.JobStatusSuccess, wantSource: false},
		{name: "partial success keeps the source", deleteSource: true, terminal: models.JobStatusPartialSuccess, wantSource: true},
		{name: "failed keeps the source", deleteSource: true, terminal: models.JobStatusFailed, wantSource: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := newFakeRecipeRunner()
			runner.terminalStatus = test.terminal
			service, store := newTestService(t, runner, nil)

			watch := t.TempDir()
			output := t.TempDir()
			rule := saveTestRule(t, service, models.AutomationRuleV1{
				Name:         "Inbox",
				Enabled:      true,
				WatchDir:     watch,
				RecipeID:     testRecipeID,
				OutputDir:    output,
				DeleteSource: test.deleteSource,
			})
			startService(t, service)

			source := filepath.Join(watch, "note.txt")
			writeTestFile(t, source, "delete source rules")

			waitFor(t, 3*time.Second, "the persisted terminal status", func() bool {
				stored, found := store.Get(rule.ID)

				return found && stored.LastStatus == test.terminal
			})

			_, err := os.Stat(source)
			if test.wantSource {
				if err != nil {
					t.Fatalf("expected the source to be kept, got %v", err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("expected the source to be deleted, got %v", err)
			}
		})
	}
}

func TestWatcherPollsUntilTerminal(t *testing.T) {
	runner := newFakeRecipeRunner()
	runner.pollsBeforeTerminal = 3
	service, store := newTestService(t, runner, nil)

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	writeTestFile(t, filepath.Join(watch, "polled.txt"), "poll me")

	waitFor(t, 3*time.Second, "the persisted terminal status", func() bool {
		stored, found := store.Get(rule.ID)

		return found && stored.LastStatus == models.JobStatusSuccess
	})
	if runner.runCount() != 1 {
		t.Fatalf("expected one run, got %d", runner.runCount())
	}
}

func TestShutdownStopsWatcher(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})

	service.Start(context.Background())
	time.Sleep(50 * time.Millisecond)
	service.Shutdown()

	writeTestFile(t, filepath.Join(watch, "late.txt"), "too late")
	assertRunStaysAt(t, runner, 0, 300*time.Millisecond)

	rules := service.ListRules().Rules
	if len(rules) != 1 || rules[0].RuntimeStatus != RuntimeStatusStopped {
		t.Fatalf("expected a stopped rule after shutdown, got %+v", rules)
	}
}

func TestShutdownDuringRunRecordsCancelled(t *testing.T) {
	runner := newFakeRecipeRunner()
	runner.pollsBeforeTerminal = 100000
	service, store := newTestService(t, runner, nil)

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})

	service.Start(context.Background())
	t.Cleanup(service.Shutdown)
	time.Sleep(50 * time.Millisecond)

	source := filepath.Join(watch, "slow.txt")
	writeTestFile(t, source, "slow run")

	waitFor(t, 3*time.Second, "the recipe to start polling", func() bool {
		return runner.statusCallCount() > 0
	})

	service.Shutdown()

	stored, found := store.Get(rule.ID)
	if !found {
		t.Fatal("rule disappeared from the store")
	}
	if stored.LastStatus != models.JobStatusCancelled {
		t.Fatalf("expected a cancelled last run, got %+v", stored)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("a cancelled run must keep the source: %v", err)
	}
}

func TestWatcherErrorMarksRuleDegradedAndCanRecover(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	service.mu.Lock()
	watcher := service.watchers[rule.ID]
	service.mu.Unlock()
	if watcher == nil {
		t.Fatal("expected a running watcher")
	}

	watcher.watcher.Errors <- errors.New("inotify queue overflow")

	waitFor(t, 3*time.Second, "the rule to be degraded", func() bool {
		rules := service.ListRules().Rules

		return len(rules) == 1 && rules[0].RuntimeStatus == RuntimeStatusDegraded
	})
	rules := service.ListRules().Rules
	if rules[0].RuntimeReason == "" {
		t.Fatalf("degraded rule must carry a reason: %+v", rules[0])
	}

	// Re-enabling installs a fresh watcher because the previous one is dead.
	response := service.SetRuleEnabled(rule.ID, true)
	if !response.Success || response.Rule == nil || response.Rule.RuntimeStatus != RuntimeStatusWatching {
		t.Fatalf("expected a fresh watcher, got %+v", response)
	}
	writeTestFile(t, filepath.Join(watch, "recovered.txt"), "recovered")
	waitForRunCount(t, runner, 1, 3*time.Second)
}

func TestWatcherRecordsPlanFailure(t *testing.T) {
	runner := newFakeRecipeRunner()
	runner.failPlan = true
	runner.planMessage = "The test plan rejected this file."
	service, store := newTestService(t, runner, nil)

	watch := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRuleV1{
		Name:     "Inbox",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	startService(t, service)

	writeTestFile(t, filepath.Join(watch, "rejected.txt"), "plan failure")

	waitFor(t, 3*time.Second, "the failed last run", func() bool {
		stored, found := store.Get(rule.ID)

		return found && stored.LastStatus == models.JobStatusFailed
	})
	stored, _ := store.Get(rule.ID)
	if stored.LastMessage != "The test plan rejected this file." {
		t.Fatalf("unexpected failure message: %q", stored.LastMessage)
	}
	if runner.runCount() != 0 {
		t.Fatalf("a plan failure must not run the recipe, got %d runs", runner.runCount())
	}
}
