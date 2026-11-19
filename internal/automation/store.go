package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/fsx"
)

const (
	defaultMaxRules = 100
	maxLastOutputs  = 10

	filePermissions = 0o644

	rulesFileName = "automation_rules_v1.json"
)

// ErrRuleNotFound is returned when a rule id is not in the store.
var ErrRuleNotFound = errors.New("automation: rule not found")

// Store keeps the automation rules in memory and persists them atomically as a
// JSON array. The file is capped and tolerant: a missing or malformed file
// starts empty instead of failing the app.
//
// writeMu orders mutations with their disk writes: the state lock only guards
// the in-memory slice, while marshal and fsync run without blocking readers.
type Store struct {
	mu       sync.RWMutex
	writeMu  sync.Mutex
	path     string
	maxRules int
	rules    []models.AutomationRuleV1
}

// DefaultPath returns the persisted rules location:
// os.UserConfigDir()/fileforge/automation_rules_v1.json, with a repo-local
// fallback when the platform config directory is unavailable.
func DefaultPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(".", ".fileforge", rulesFileName)
	}

	return filepath.Join(configDir, "fileforge", rulesFileName)
}

// NewStore loads the rules at path, tolerating a missing or malformed file.
func NewStore(path string) *Store {
	return newStore(path, defaultMaxRules)
}

func newStore(path string, maxRules int) *Store {
	store := &Store{
		path:     strings.TrimSpace(path),
		maxRules: maxRules,
	}
	store.load()

	return store
}

func (s *Store) configured() bool {
	return s.path != ""
}

// List returns the saved rules newest first. Callers own the returned copies.
func (s *Store) List() []models.AutomationRuleV1 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rules := make([]models.AutomationRuleV1, len(s.rules))
	for index, rule := range s.rules {
		rules[index] = cloneRule(rule)
	}

	return rules
}

// Get returns one rule copy.
func (s *Store) Get(id string) (models.AutomationRuleV1, bool) {
	trimmed := strings.TrimSpace(id)

	s.mu.RLock()
	defer s.mu.RUnlock()

	index := s.findIndexLocked(trimmed)
	if index < 0 {
		return models.AutomationRuleV1{}, false
	}

	return cloneRule(s.rules[index]), true
}

// Save inserts or replaces a rule and persists the store. Runtime-only fields
// are cleared and LastOutputs is capped before writing. New rules are placed
// first; edits keep their position. The disk write happens outside the state
// lock so readers never wait for fsync.
func (s *Store) Save(rule models.AutomationRuleV1) (models.AutomationRuleV1, error) {
	rule = cloneRule(rule)
	rule.ID = strings.TrimSpace(rule.ID)
	if rule.ID == "" {
		return models.AutomationRuleV1{}, errors.New("automation: rule id is required")
	}
	rule.RuntimeStatus = ""
	rule.RuntimeReason = ""
	if len(rule.LastOutputs) > maxLastOutputs {
		rule.LastOutputs = rule.LastOutputs[:maxLastOutputs]
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	previous := s.snapshotLocked()
	if index := s.findIndexLocked(rule.ID); index >= 0 {
		s.rules[index] = rule
	} else {
		s.rules = append([]models.AutomationRuleV1{rule}, s.rules...)
	}
	s.pruneLocked()
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.saveRules(snapshot); err != nil {
		s.mu.Lock()
		s.rules = previous
		s.mu.Unlock()
		return models.AutomationRuleV1{}, err
	}

	return cloneRule(rule), nil
}

// Delete removes a rule by id and persists the store.
func (s *Store) Delete(id string) error {
	trimmed := strings.TrimSpace(id)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	index := s.findIndexLocked(trimmed)
	if index < 0 {
		s.mu.Unlock()
		return ErrRuleNotFound
	}

	previous := s.snapshotLocked()
	s.rules = append(s.rules[:index], s.rules[index+1:]...)
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.saveRules(snapshot); err != nil {
		s.mu.Lock()
		s.rules = previous
		s.mu.Unlock()
		return err
	}

	return nil
}

// UpdateRun records the terminal outcome of the last run for a rule.
func (s *Store) UpdateRun(id string, at int64, status, message, file string, outputs []string) error {
	trimmed := strings.TrimSpace(id)
	if len(outputs) > maxLastOutputs {
		outputs = outputs[:maxLastOutputs]
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	index := s.findIndexLocked(trimmed)
	if index < 0 {
		s.mu.Unlock()
		return ErrRuleNotFound
	}

	previous := s.snapshotLocked()
	rule := &s.rules[index]
	rule.LastRunAt = at
	rule.LastStatus = strings.TrimSpace(status)
	rule.LastMessage = strings.TrimSpace(message)
	rule.LastFile = strings.TrimSpace(file)
	rule.LastOutputs = append([]string{}, outputs...)
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.saveRules(snapshot); err != nil {
		s.mu.Lock()
		s.rules = previous
		s.mu.Unlock()
		return err
	}

	return nil
}

// SetEnabled flips the enabled flag of a rule and persists the store.
func (s *Store) SetEnabled(id string, enabled bool) error {
	trimmed := strings.TrimSpace(id)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	index := s.findIndexLocked(trimmed)
	if index < 0 {
		s.mu.Unlock()
		return ErrRuleNotFound
	}

	previous := s.snapshotLocked()
	s.rules[index].Enabled = enabled
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.saveRules(snapshot); err != nil {
		s.mu.Lock()
		s.rules = previous
		s.mu.Unlock()
		return err
	}

	return nil
}

func (s *Store) load() {
	if !s.configured() {
		s.rules = []models.AutomationRuleV1{}
		return
	}

	content, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("automation.store.load.failed path=%s err=%v", s.path, err)
		}
		s.rules = []models.AutomationRuleV1{}
		return
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		s.rules = []models.AutomationRuleV1{}
		return
	}

	rules := make([]models.AutomationRuleV1, 0)
	if err := json.Unmarshal(content, &rules); err != nil {
		log.Printf("automation.store.load.malformed path=%s err=%v", s.path, err)
		s.rules = []models.AutomationRuleV1{}
		return
	}

	s.rules = normalizeRules(rules)
	s.pruneLocked()
}

func normalizeRules(rules []models.AutomationRuleV1) []models.AutomationRuleV1 {
	normalized := make([]models.AutomationRuleV1, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		id := strings.TrimSpace(rule.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}

		rule = cloneRule(rule)
		rule.ID = id
		rule.RuntimeStatus = ""
		rule.RuntimeReason = ""
		if len(rule.LastOutputs) > maxLastOutputs {
			rule.LastOutputs = rule.LastOutputs[:maxLastOutputs]
		}
		normalized = append(normalized, rule)
	}

	return normalized
}

func (s *Store) pruneLocked() {
	if s.maxRules > 0 && len(s.rules) > s.maxRules {
		s.rules = s.rules[:s.maxRules]
	}
}

// snapshotLocked returns a deep copy of the rules. The caller holds s.mu.
func (s *Store) snapshotLocked() []models.AutomationRuleV1 {
	rules := make([]models.AutomationRuleV1, len(s.rules))
	for index, rule := range s.rules {
		rules[index] = cloneRule(rule)
	}

	return rules
}

// saveRules marshals and writes one rules snapshot. It never runs under s.mu;
// writeMu keeps concurrent snapshots from being published out of order.
func (s *Store) saveRules(rules []models.AutomationRuleV1) error {
	if !s.configured() {
		return errors.New("automation: rules path is not configured")
	}

	payload, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return fmt.Errorf("automation: encode rules: %w", err)
	}
	if err := fsx.WriteAtomic(s.path, payload, filePermissions); err != nil {
		return fmt.Errorf("automation: persist rules: %w", err)
	}

	return nil
}

func (s *Store) findIndexLocked(id string) int {
	for index := range s.rules {
		if s.rules[index].ID == id {
			return index
		}
	}

	return -1
}

func cloneRule(rule models.AutomationRuleV1) models.AutomationRuleV1 {
	cloned := rule
	cloned.Extensions = append([]string{}, rule.Extensions...)
	cloned.LastOutputs = append([]string{}, rule.LastOutputs...)
	if rule.Options != nil {
		cloned.Options = make(map[string]any, len(rule.Options))
		for key, value := range rule.Options {
			cloned.Options[key] = value
		}
	}

	return cloned
}
