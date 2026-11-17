package automation

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

func TestStoreSavesAndReloadsRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automation_rules_v1.json")
	store := NewStore(path)

	rule := models.AutomationRuleV1{
		ID:            "rule_1",
		Name:          "Inbox",
		Enabled:       true,
		WatchDir:      "/tmp/inbox",
		RecipeID:      testRecipeID,
		Extensions:    []string{"txt"},
		OutputDir:     "/tmp/out",
		Options:       map[string]any{"quality": float64(80)},
		DeleteSource:  true,
		CreatedAt:     1000,
		LastStatus:    "",
		RuntimeStatus: RuntimeStatusWatching,
		RuntimeReason: "should not persist",
	}
	if _, err := store.Save(rule); err != nil {
		t.Fatalf("save rule: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected rules file to exist: %v", err)
	}

	reloaded := NewStore(path)
	rules := reloaded.List()
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule after reload, got %d", len(rules))
	}
	loaded := rules[0]
	if loaded.ID != rule.ID || loaded.Name != rule.Name || !loaded.Enabled {
		t.Fatalf("unexpected reloaded rule identity: %+v", loaded)
	}
	if loaded.WatchDir != rule.WatchDir || loaded.OutputDir != rule.OutputDir || loaded.RecipeID != rule.RecipeID {
		t.Fatalf("unexpected reloaded paths: %+v", loaded)
	}
	if len(loaded.Extensions) != 1 || loaded.Extensions[0] != "txt" {
		t.Fatalf("unexpected reloaded extensions: %+v", loaded.Extensions)
	}
	if !loaded.DeleteSource || loaded.CreatedAt != 1000 {
		t.Fatalf("unexpected reloaded flags: %+v", loaded)
	}
	if loaded.RuntimeStatus != "" || loaded.RuntimeReason != "" {
		t.Fatalf("runtime-only fields must not persist: %+v", loaded)
	}
}

func TestStoreDeleteRemovesRule(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automation_rules_v1.json")
	store := NewStore(path)

	for _, id := range []string{"rule_1", "rule_2"} {
		if _, err := store.Save(models.AutomationRuleV1{ID: id, Name: id}); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}

	if err := store.Delete("rule_1"); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if _, found := store.Get("rule_1"); found {
		t.Fatal("deleted rule is still present")
	}
	if err := store.Delete("rule_1"); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("expected ErrRuleNotFound on second delete, got %v", err)
	}

	reloaded := NewStore(path)
	rules := reloaded.List()
	if len(rules) != 1 || rules[0].ID != "rule_2" {
		t.Fatalf("unexpected reloaded rules: %+v", rules)
	}
}

func TestStoreUpdateRunCapsOutputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automation_rules_v1.json")
	store := NewStore(path)

	if _, err := store.Save(models.AutomationRuleV1{ID: "rule_1", Name: "Rule"}); err != nil {
		t.Fatalf("save rule: %v", err)
	}

	outputs := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		outputs = append(outputs, filepath.Join("/tmp/out", "file-"+string(rune('a'+index))+".txt"))
	}
	if err := store.UpdateRun("rule_1", 5000, models.JobStatusSuccess, "done", "/tmp/in.txt", outputs); err != nil {
		t.Fatalf("update run: %v", err)
	}

	rule, found := store.Get("rule_1")
	if !found {
		t.Fatal("rule is missing")
	}
	if rule.LastRunAt != 5000 || rule.LastStatus != models.JobStatusSuccess || rule.LastMessage != "done" || rule.LastFile != "/tmp/in.txt" {
		t.Fatalf("unexpected last run fields: %+v", rule)
	}
	if len(rule.LastOutputs) != maxLastOutputs {
		t.Fatalf("expected %d capped outputs, got %d", maxLastOutputs, len(rule.LastOutputs))
	}

	reloaded := NewStore(path)
	reloadedRule, found := reloaded.Get("rule_1")
	if !found || reloadedRule.LastRunAt != 5000 || len(reloadedRule.LastOutputs) != maxLastOutputs {
		t.Fatalf("last run did not persist: %+v", reloadedRule)
	}

	if err := store.UpdateRun("missing", 1, models.JobStatusFailed, "", "", nil); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("expected ErrRuleNotFound, got %v", err)
	}
}

func TestStoreSetEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automation_rules_v1.json")
	store := NewStore(path)

	if _, err := store.Save(models.AutomationRuleV1{ID: "rule_1", Name: "Rule", Enabled: false}); err != nil {
		t.Fatalf("save rule: %v", err)
	}
	if err := store.SetEnabled("rule_1", true); err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	rule, found := store.Get("rule_1")
	if !found || !rule.Enabled {
		t.Fatalf("expected rule to be enabled: %+v", rule)
	}

	reloaded := NewStore(path)
	reloadedRule, found := reloaded.Get("rule_1")
	if !found || !reloadedRule.Enabled {
		t.Fatal("enabled flag did not persist")
	}

	if err := store.SetEnabled("missing", true); !errors.Is(err, ErrRuleNotFound) {
		t.Fatalf("expected ErrRuleNotFound, got %v", err)
	}
}

func TestStoreToleratesMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automation_rules_v1.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write malformed file: %v", err)
	}

	store := NewStore(path)
	if rules := store.List(); len(rules) != 0 {
		t.Fatalf("expected an empty list, got %+v", rules)
	}

	if _, err := store.Save(models.AutomationRuleV1{ID: "rule_1", Name: "Rule"}); err != nil {
		t.Fatalf("save after malformed load: %v", err)
	}
	if rules := NewStore(path).List(); len(rules) != 1 {
		t.Fatalf("expected the store to recover, got %+v", rules)
	}
}

func TestStoreCapsRuleCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automation_rules_v1.json")
	store := newStore(path, 3)

	for index := 0; index < 5; index++ {
		id := "rule_" + string(rune('0'+index))
		if _, err := store.Save(models.AutomationRuleV1{ID: id, Name: id}); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}

	rules := store.List()
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules after cap, got %d", len(rules))
	}
	if rules[0].ID != "rule_4" || rules[1].ID != "rule_3" || rules[2].ID != "rule_2" {
		t.Fatalf("expected newest rules first, got %+v", rules)
	}
}

func TestStoreSaveRequiresID(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "automation_rules_v1.json"))
	if _, err := store.Save(models.AutomationRuleV1{Name: "No id"}); err == nil {
		t.Fatal("expected a missing-id error")
	}
}

func TestDefaultPathUsesRulesFileName(t *testing.T) {
	path := DefaultPath()
	if !strings.HasSuffix(path, rulesFileName) {
		t.Fatalf("unexpected default path: %s", path)
	}
}
