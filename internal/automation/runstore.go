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
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/fsx"
)

const (
	defaultMaxRuns      = 100
	defaultRunListLimit = 20
	maxRunOutputs       = 10

	runsFileName = "automation_runs_v1.json"
)

// DefaultRunPath returns the persisted run history location:
// os.UserConfigDir()/fileforge/automation_runs_v1.json, with a repo-local
// fallback when the platform config directory is unavailable.
func DefaultRunPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(".", ".fileforge", runsFileName)
	}

	return filepath.Join(configDir, "fileforge", runsFileName)
}

// RunStore keeps the recent automation runs in memory and persists them
// atomically as a JSON array. The file is capped and tolerant: a missing or
// malformed file starts empty instead of failing the app.
//
// writeMu orders mutations with their disk writes: the state lock only guards
// the in-memory slice, while marshal and fsync run without blocking readers.
type RunStore struct {
	mu      sync.RWMutex
	writeMu sync.Mutex
	path    string
	maxRuns int
	runs    []models.AutomationRunRecordV1
}

// NewRunStore loads the run history at path, tolerating a missing or malformed
// file.
func NewRunStore(path string) *RunStore {
	return newRunStore(path, defaultMaxRuns)
}

func newRunStore(path string, maxRuns int) *RunStore {
	store := &RunStore{
		path:    strings.TrimSpace(path),
		maxRuns: maxRuns,
	}
	store.load()

	return store
}

// Record prepends one terminal run, capping its outputs and persisting the
// store. An empty id is replaced by a generated run id.
func (s *RunStore) Record(record models.AutomationRunRecordV1) {
	if s == nil {
		return
	}

	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		record.ID = fmt.Sprintf("run_%d", time.Now().UnixNano())
	}
	record.RuleID = strings.TrimSpace(record.RuleID)
	record.RuleName = strings.TrimSpace(record.RuleName)
	record.FilePath = strings.TrimSpace(record.FilePath)
	record.Status = strings.TrimSpace(record.Status)
	record.Message = strings.TrimSpace(record.Message)
	if len(record.Outputs) > maxRunOutputs {
		record.Outputs = record.Outputs[:maxRunOutputs]
	}
	record.Outputs = append([]string{}, record.Outputs...)
	if record.At <= 0 {
		record.At = time.Now().UnixMilli()
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	s.runs = append([]models.AutomationRunRecordV1{record}, s.runs...)
	s.pruneLocked()
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.saveRuns(snapshot); err != nil {
		log.Printf("automation.runs.record.failed path=%s err=%v", s.path, err)
	}
}

// List returns the recorded runs newest first. A zero limit returns the default
// page size; the result is clamped to the store capacity.
func (s *RunStore) List(limit int) []models.AutomationRunRecordV1 {
	if s == nil {
		return []models.AutomationRunRecordV1{}
	}
	switch {
	case limit == 0:
		limit = defaultRunListLimit
	case limit < 0:
		limit = 1
	}
	if s.maxRuns > 0 && limit > s.maxRuns {
		limit = s.maxRuns
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit > len(s.runs) {
		limit = len(s.runs)
	}

	runs := make([]models.AutomationRunRecordV1, limit)
	for index := 0; index < limit; index++ {
		runs[index] = cloneRunRecord(s.runs[index])
	}

	return runs
}

// Clear drops every recorded run and persists the empty store.
func (s *RunStore) Clear() error {
	if s == nil {
		return nil
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	s.mu.Lock()
	previous := s.snapshotLocked()
	s.runs = []models.AutomationRunRecordV1{}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if err := s.saveRuns(snapshot); err != nil {
		s.mu.Lock()
		s.runs = previous
		s.mu.Unlock()
		return err
	}

	return nil
}

func (s *RunStore) load() {
	if s.path == "" {
		s.runs = []models.AutomationRunRecordV1{}
		return
	}

	content, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("automation.runs.load.failed path=%s err=%v", s.path, err)
		}
		s.runs = []models.AutomationRunRecordV1{}
		return
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		s.runs = []models.AutomationRunRecordV1{}
		return
	}

	runs := make([]models.AutomationRunRecordV1, 0)
	if err := json.Unmarshal(content, &runs); err != nil {
		log.Printf("automation.runs.load.malformed path=%s err=%v", s.path, err)
		s.runs = []models.AutomationRunRecordV1{}
		return
	}

	s.runs = normalizeRuns(runs)
	s.pruneLocked()
}

func normalizeRuns(runs []models.AutomationRunRecordV1) []models.AutomationRunRecordV1 {
	normalized := make([]models.AutomationRunRecordV1, 0, len(runs))
	seen := make(map[string]struct{}, len(runs))
	for _, record := range runs {
		id := strings.TrimSpace(record.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}

		record = cloneRunRecord(record)
		record.ID = id
		if len(record.Outputs) > maxRunOutputs {
			record.Outputs = record.Outputs[:maxRunOutputs]
		}
		normalized = append(normalized, record)
	}

	return normalized
}

func (s *RunStore) pruneLocked() {
	if s.maxRuns > 0 && len(s.runs) > s.maxRuns {
		s.runs = s.runs[:s.maxRuns]
	}
}

// snapshotLocked returns a deep copy of the runs. The caller holds s.mu.
func (s *RunStore) snapshotLocked() []models.AutomationRunRecordV1 {
	runs := make([]models.AutomationRunRecordV1, len(s.runs))
	for index, record := range s.runs {
		runs[index] = cloneRunRecord(record)
	}

	return runs
}

// saveRuns marshals and writes one runs snapshot. It never runs under s.mu;
// writeMu keeps concurrent snapshots from being published out of order.
func (s *RunStore) saveRuns(runs []models.AutomationRunRecordV1) error {
	if s.path == "" {
		return nil
	}

	payload, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return fmt.Errorf("automation: encode runs: %w", err)
	}
	if err := fsx.WriteAtomic(s.path, payload, filePermissions); err != nil {
		return fmt.Errorf("automation: persist runs: %w", err)
	}

	return nil
}

func cloneRunRecord(record models.AutomationRunRecordV1) models.AutomationRunRecordV1 {
	cloned := record
	cloned.Outputs = append([]string{}, record.Outputs...)

	return cloned
}
