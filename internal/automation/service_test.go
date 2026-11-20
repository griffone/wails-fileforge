package automation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

func TestSaveRuleValidation(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	output := t.TempDir()
	watchFile := filepath.Join(t.TempDir(), "not-a-directory.txt")
	if err := os.WriteFile(watchFile, []byte("file"), 0o644); err != nil {
		t.Fatalf("write watch file: %v", err)
	}

	base := func() models.AutomationRule {
		return models.AutomationRule{
			Name:     "Docs",
			WatchDir: watch,
			RecipeID: testRecipeID,
		}
	}

	tests := []struct {
		name   string
		mutate func(*models.AutomationRule)
		want   bool
	}{
		{name: "valid minimal rule", mutate: func(*models.AutomationRule) {}, want: true},
		{name: "missing name", mutate: func(rule *models.AutomationRule) { rule.Name = "   " }, want: false},
		{name: "missing watch dir", mutate: func(rule *models.AutomationRule) { rule.WatchDir = "" }, want: false},
		{name: "watch dir does not exist", mutate: func(rule *models.AutomationRule) {
			rule.WatchDir = filepath.Join(watch, "missing")
		}, want: false},
		{name: "watch path is a file", mutate: func(rule *models.AutomationRule) { rule.WatchDir = watchFile }, want: false},
		{name: "unknown recipe", mutate: func(rule *models.AutomationRule) { rule.RecipeID = "recipe.unknown" }, want: false},
		{name: "missing recipe", mutate: func(rule *models.AutomationRule) { rule.RecipeID = "" }, want: false},
		{name: "unsupported extension", mutate: func(rule *models.AutomationRule) {
			rule.Extensions = []string{"txt", "png"}
		}, want: false},
		{name: "output dir does not exist", mutate: func(rule *models.AutomationRule) {
			rule.OutputDir = filepath.Join(output, "missing")
		}, want: false},
		{name: "output path is a file", mutate: func(rule *models.AutomationRule) { rule.OutputDir = watchFile }, want: false},
		{name: "valid explicit output dir", mutate: func(rule *models.AutomationRule) { rule.OutputDir = output }, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rule := base()
			test.mutate(&rule)
			response := service.SaveRule(rule)

			if response.Success != test.want {
				t.Fatalf("Success = %t, want %t (message: %s, error: %+v)", response.Success, test.want, response.Message, response.Error)
			}
			if !test.want {
				if response.Message == "" || response.Error == nil {
					t.Fatalf("expected a message and a canonical error, got %+v", response)
				}

				return
			}
			if response.Rule == nil {
				t.Fatal("successful save must return the rule")
			}
		})
	}
}

func TestSaveRuleNormalizesAndPreservesTimestamps(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	output := t.TempDir()

	saved := saveTestRule(t, service, models.AutomationRule{
		Name:       "  Inbox  ",
		WatchDir:   watch + string(filepath.Separator) + ".",
		RecipeID:   testRecipeID,
		Extensions: []string{".TXT", "txt", " ", ".Txt"},
		OutputDir:  output + string(filepath.Separator) + ".",
	})

	if saved.Name != "Inbox" {
		t.Fatalf("expected a trimmed name, got %q", saved.Name)
	}
	if saved.WatchDir != filepath.Clean(watch) {
		t.Fatalf("expected a cleaned absolute watch dir, got %q", saved.WatchDir)
	}
	if saved.OutputDir != filepath.Clean(output) {
		t.Fatalf("expected a cleaned absolute output dir, got %q", saved.OutputDir)
	}
	if len(saved.Extensions) != 1 || saved.Extensions[0] != "txt" {
		t.Fatalf("expected normalized extensions, got %+v", saved.Extensions)
	}
	if !strings.HasPrefix(saved.ID, "rule_") || saved.CreatedAt <= 0 || saved.UpdatedAt <= 0 {
		t.Fatalf("expected a generated id and timestamps, got %+v", saved)
	}

	edited := saved
	edited.Name = "Renamed"
	response := service.SaveRule(edited)
	if !response.Success || response.Rule == nil {
		t.Fatalf("edit failed: %s", response.Message)
	}
	if response.Rule.CreatedAt != saved.CreatedAt {
		t.Fatalf("edit must preserve createdAt: got %d want %d", response.Rule.CreatedAt, saved.CreatedAt)
	}
	if response.Rule.UpdatedAt < saved.UpdatedAt {
		t.Fatalf("edit must refresh updatedAt: got %d want >= %d", response.Rule.UpdatedAt, saved.UpdatedAt)
	}
	if response.Rule.ID != saved.ID || response.Rule.Name != "Renamed" {
		t.Fatalf("unexpected edited rule: %+v", response.Rule)
	}

	missing := models.AutomationRule{ID: "rule_missing", Name: "Ghost", WatchDir: watch, RecipeID: testRecipeID}
	if response := service.SaveRule(missing); response.Success {
		t.Fatal("expected saving an unknown rule id to fail")
	}
}

func TestRuleLifecycleTogglesWatcher(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, store := newTestService(t, runner, nil)

	watch := t.TempDir()
	output := t.TempDir()
	rule := saveTestRule(t, service, models.AutomationRule{
		Name:      "Inbox",
		Enabled:   false,
		WatchDir:  watch,
		RecipeID:  testRecipeID,
		OutputDir: output,
	})

	startService(t, service)

	// Disabled rule: the file is ignored.
	writeTestFile(t, filepath.Join(watch, "disabled.txt"), "one")
	assertRunStaysAt(t, runner, 0, 250*time.Millisecond)

	// Enable: a fresh watcher must process the next file.
	response := service.SetRuleEnabled(rule.ID, true)
	if !response.Success || response.Rule == nil {
		t.Fatalf("enable failed: %s", response.Message)
	}
	if !response.Rule.Enabled || response.Rule.RuntimeStatus != RuntimeStatusWatching {
		t.Fatalf("unexpected enabled rule state: %+v", response.Rule)
	}
	writeTestFile(t, filepath.Join(watch, "enabled.txt"), "two")
	waitForRunCount(t, runner, 1, 3*time.Second)
	waitFor(t, 3*time.Second, "the last run to be persisted", func() bool {
		stored, found := store.Get(rule.ID)

		return found && stored.LastStatus == models.JobStatusSuccess
	})

	status := service.Status()
	if status.TotalRules != 1 || status.ActiveRules != 1 {
		t.Fatalf("unexpected status after enable: %+v", status)
	}
	if status.LastRunAt == 0 || status.LastStatus != models.JobStatusSuccess || status.LastFile != filepath.Join(watch, "enabled.txt") {
		t.Fatalf("unexpected last run in status: %+v", status)
	}

	// Disable: the watcher stops and no further runs happen.
	response = service.SetRuleEnabled(rule.ID, false)
	if !response.Success || response.Rule == nil {
		t.Fatalf("disable failed: %s", response.Message)
	}
	if response.Rule.Enabled || response.Rule.RuntimeStatus != RuntimeStatusStopped {
		t.Fatalf("unexpected disabled rule state: %+v", response.Rule)
	}
	writeTestFile(t, filepath.Join(watch, "disabled-again.txt"), "three")
	assertRunStaysAt(t, runner, 1, 250*time.Millisecond)

	// Delete: the rule disappears from the store and the list.
	response = service.DeleteRule(rule.ID)
	if !response.Success {
		t.Fatalf("delete failed: %s", response.Message)
	}
	if _, found := store.Get(rule.ID); found {
		t.Fatal("deleted rule is still in the store")
	}
	if list := service.ListRules(); len(list.Rules) != 0 {
		t.Fatalf("expected an empty rule list, got %+v", list.Rules)
	}
	if response := service.DeleteRule(rule.ID); response.Success {
		t.Fatal("deleting a missing rule must fail")
	}
	if response := service.SetRuleEnabled("rule_missing", true); response.Success {
		t.Fatal("toggling a missing rule must fail")
	}
}

func TestListRulesReportsRuntimeState(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := t.TempDir()
	enabled := saveTestRule(t, service, models.AutomationRule{
		Name:     "Enabled",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	disabled := saveTestRule(t, service, models.AutomationRule{
		Name:     "Disabled",
		Enabled:  false,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})

	startService(t, service)

	statuses := map[string]models.AutomationRule{}
	for _, rule := range service.ListRules().Rules {
		statuses[rule.ID] = rule
	}
	if len(statuses) != 2 {
		t.Fatalf("expected two rules, got %+v", statuses)
	}
	if statuses[enabled.ID].RuntimeStatus != RuntimeStatusWatching {
		t.Fatalf("enabled rule should be watching: %+v", statuses[enabled.ID])
	}
	if statuses[disabled.ID].RuntimeStatus != RuntimeStatusStopped {
		t.Fatalf("disabled rule should be stopped: %+v", statuses[disabled.ID])
	}
}

func TestStartToleratesMissingWatchFolder(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	watch := filepath.Join(t.TempDir(), "watch")
	if err := os.Mkdir(watch, 0o755); err != nil {
		t.Fatalf("create watch dir: %v", err)
	}
	rule := saveTestRule(t, service, models.AutomationRule{
		Name:     "Gone",
		Enabled:  true,
		WatchDir: watch,
		RecipeID: testRecipeID,
	})
	if err := os.RemoveAll(watch); err != nil {
		t.Fatalf("remove watch dir: %v", err)
	}

	service.Start(context.Background())
	t.Cleanup(service.Shutdown)

	list := service.ListRules()
	if len(list.Rules) != 1 {
		t.Fatalf("expected the rule to stay listed, got %+v", list.Rules)
	}
	if list.Rules[0].ID != rule.ID {
		t.Fatalf("unexpected listed rule: %+v", list.Rules[0])
	}
	if list.Rules[0].RuntimeStatus != RuntimeStatusDegraded || list.Rules[0].RuntimeReason == "" {
		t.Fatalf("expected a degraded rule with a reason, got %+v", list.Rules[0])
	}

	status := service.Status()
	if status.TotalRules != 1 || status.ActiveRules != 0 {
		t.Fatalf("unexpected status for a degraded rule: %+v", status)
	}
	if runner.runCount() != 0 {
		t.Fatalf("degraded rule must not run anything, got %d runs", runner.runCount())
	}
}

func TestEmptyStatus(t *testing.T) {
	runner := newFakeRecipeRunner()
	service, _ := newTestService(t, runner, nil)

	status := service.Status()
	if !status.Success || status.TotalRules != 0 || status.ActiveRules != 0 {
		t.Fatalf("unexpected empty status: %+v", status)
	}
	list := service.ListRules()
	if !list.Success || len(list.Rules) != 0 {
		t.Fatalf("unexpected empty list: %+v", list)
	}
}
