package automation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

func TestNewStoreMigratesLegacyRulesFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacyRulesFileName)
	currentPath := filepath.Join(dir, rulesFileName)
	writeRulesFile(t, legacyPath, "rule_legacy")

	store := NewStore(currentPath)

	rules := store.List()
	if len(rules) != 1 || rules[0].ID != "rule_legacy" {
		t.Fatalf("expected the legacy rule to be loaded, got %+v", rules)
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("expected the migrated file to exist: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("expected the legacy file to be preserved: %v", err)
	}
}

func TestNewStoreKeepsExistingRulesFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacyRulesFileName)
	currentPath := filepath.Join(dir, rulesFileName)
	writeRulesFile(t, legacyPath, "rule_legacy")
	original := writeRulesFile(t, currentPath, "rule_current")

	store := NewStore(currentPath)

	rules := store.List()
	if len(rules) != 1 || rules[0].ID != "rule_current" {
		t.Fatalf("expected the current rule to win, got %+v", rules)
	}
	content, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("read current file: %v", err)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("expected the current file to stay untouched, got %q", content)
	}
}

func TestNewStoreWithoutAnyRulesFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, rulesFileName)

	store := NewStore(currentPath)

	if rules := store.List(); len(rules) != 0 {
		t.Fatalf("expected no rules, got %+v", rules)
	}
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("expected no rules file to be created, got err=%v", err)
	}
}

func TestNewRunStoreMigratesLegacyRunsFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacyRunsFileName)
	currentPath := filepath.Join(dir, runsFileName)
	writeRunsFile(t, legacyPath, "run_legacy")

	store := NewRunStore(currentPath)

	runs := store.List(0)
	if len(runs) != 1 || runs[0].ID != "run_legacy" {
		t.Fatalf("expected the legacy run to be loaded, got %+v", runs)
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("expected the migrated file to exist: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("expected the legacy file to be preserved: %v", err)
	}
}

func TestNewRunStoreKeepsExistingRunsFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacyRunsFileName)
	currentPath := filepath.Join(dir, runsFileName)
	writeRunsFile(t, legacyPath, "run_legacy")
	original := writeRunsFile(t, currentPath, "run_current")

	store := NewRunStore(currentPath)

	runs := store.List(0)
	if len(runs) != 1 || runs[0].ID != "run_current" {
		t.Fatalf("expected the current run to win, got %+v", runs)
	}
	content, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("read current file: %v", err)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("expected the current file to stay untouched, got %q", content)
	}
}

func TestNewRunStoreWithoutAnyRunsFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, runsFileName)

	store := NewRunStore(currentPath)

	if runs := store.List(0); len(runs) != 0 {
		t.Fatalf("expected no runs, got %+v", runs)
	}
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("expected no runs file to be created, got err=%v", err)
	}
}

// TestStoreLoadsRulesWrittenBeforeRecursivePatterns proves the new rule fields
// migrate by absence: a file written by the previous version loads with
// recursion off and no patterns, and saving it back keeps the old shape.
func TestStoreLoadsRulesWrittenBeforeRecursivePatterns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, rulesFileName)
	legacy := `[
  {
    "id": "rule_legacy",
    "name": "Legacy",
    "enabled": true,
    "watchDir": "/tmp/watch",
    "recipeId": "recipe.email_pdf",
    "extensions": ["pdf"],
    "createdAt": 1
  }
]`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy rules: %v", err)
	}

	store := NewStore(path)
	rule, found := store.Get("rule_legacy")
	if !found {
		t.Fatal("legacy rule was not loaded")
	}
	if rule.Recursive {
		t.Fatalf("a legacy rule must load with recursion off: %+v", rule)
	}
	if len(rule.Patterns) != 0 {
		t.Fatalf("a legacy rule must load with no patterns: %+v", rule.Patterns)
	}
	if len(rule.Extensions) != 1 || rule.Extensions[0] != "pdf" {
		t.Fatalf("legacy extensions changed: %+v", rule.Extensions)
	}

	// Saving the untouched rule must not introduce the new keys, so the file
	// stays readable (and unchanged in meaning) by the previous version.
	if _, err := store.Save(rule); err != nil {
		t.Fatalf("save legacy rule: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rules: %v", err)
	}
	if strings.Contains(string(content), "recursive") || strings.Contains(string(content), "patterns") {
		t.Fatalf("empty new fields must stay omitted: %s", content)
	}

	// A recursive rule with patterns round-trips.
	rule.Recursive = true
	rule.Patterns = []string{"invoice-*.pdf"}
	if _, err := store.Save(rule); err != nil {
		t.Fatalf("save recursive rule: %v", err)
	}
	reloaded, found := NewStore(path).Get("rule_legacy")
	if !found || !reloaded.Recursive || len(reloaded.Patterns) != 1 || reloaded.Patterns[0] != "invoice-*.pdf" {
		t.Fatalf("recursive rule did not persist: %+v", reloaded)
	}
}

func writeRulesFile(t *testing.T, path, ruleID string) []byte {
	t.Helper()

	rules := []models.AutomationRule{{
		ID:       ruleID,
		Name:     ruleID,
		Enabled:  true,
		WatchDir: "/tmp/watch",
		RecipeID: "recipe.email_pdf",
	}}
	payload, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("encode rules: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}

	return payload
}

func writeRunsFile(t *testing.T, path, runID string) []byte {
	t.Helper()

	runs := []models.AutomationRunRecord{{
		ID:       runID,
		RuleID:   "rule_1",
		RuleName: "Rule",
		FilePath: "/tmp/input.pdf",
		Status:   "success",
		Message:  "done",
		At:       1700000000,
	}}
	payload, err := json.Marshal(runs)
	if err != nil {
		t.Fatalf("encode runs: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write runs: %v", err)
	}

	return payload
}
