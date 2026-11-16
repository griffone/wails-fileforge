package jobhistory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/models"
)

const (
	defaultMaxEntries = 50
	defaultMaxOutputs = 500

	dirPermissions  = 0o755
	filePermissions = 0o644

	historyFileName = "job_history_v1.json"
	trashDirName    = "trash"

	reasonMissing  = "file is missing"
	reasonModified = "modified after conversion"
)

// Store keeps a capped, persisted history of terminal jobs and the output
// fingerprints needed to undo them. Undo moves untouched outputs to an
// app-managed trash directory; the app never deletes a user file.
type Store struct {
	mu         sync.RWMutex
	path       string
	trashRoot  string
	maxEntries int
	maxOutputs int
	entries    []models.JobHistoryEntryV1
	rename     func(oldPath, newPath string) error
}

// DefaultPath returns the persisted history location:
// os.UserConfigDir()/fileforge/job_history_v1.json, with a repo-local fallback
// when the platform config directory is unavailable.
func DefaultPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(".", ".fileforge", historyFileName)
	}

	return filepath.Join(configDir, "fileforge", historyFileName)
}

// NewStore loads the history at path, tolerating a missing or malformed file.
func NewStore(path string) *Store {
	return newStore(path, defaultMaxEntries, defaultMaxOutputs)
}

func newStore(path string, maxEntries, maxOutputs int) *Store {
	store := &Store{
		path:       strings.TrimSpace(path),
		maxEntries: maxEntries,
		maxOutputs: maxOutputs,
		rename:     os.Rename,
	}
	if store.path != "" {
		store.trashRoot = filepath.Join(filepath.Dir(store.path), trashDirName)
	}
	store.load()
	return store
}

func (s *Store) configured() bool {
	return s.path != ""
}

// RecordJob records the successful outputs of a terminal job. Non-terminal
// snapshots and jobs without existing outputs are ignored. A job id is
// recorded at most once.
func (s *Store) RecordJob(job models.JobResultV1) {
	if !isTerminalStatus(job.Status) {
		return
	}

	s.record(job.JobID, job.ToolID, successfulOutputs(job), job.EndedAt)
}

// RecordOutputs records explicit outputs for jobs that do not run through the
// orchestrator, such as recipe runs. The same fingerprint and dedupe rules as
// RecordJob apply.
func (s *Store) RecordOutputs(jobID, toolID string, outputs []string) {
	s.record(jobID, toolID, outputs, time.Now().UnixMilli())
}

func (s *Store) record(jobID, toolID string, outputs []string, createdAt int64) {
	if !s.configured() {
		return
	}

	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}

	records := fingerprintOutputs(outputs)
	if len(records) == 0 {
		return
	}
	if createdAt <= 0 {
		createdAt = time.Now().UnixMilli()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.findIndexLocked(jobID) >= 0 {
		return
	}

	entry := models.JobHistoryEntryV1{
		JobID:     jobID,
		ToolID:    toolID,
		CreatedAt: createdAt,
		Outputs:   records,
	}
	s.entries = append([]models.JobHistoryEntryV1{entry}, s.entries...)
	s.pruneLocked()

	if err := s.saveLocked(); err != nil {
		log.Printf("jobhistory.persist.failed path=%s jobId=%s err=%v", s.path, jobID, err)
	}
}

// List returns recorded jobs newest first.
func (s *Store) List() []models.JobHistoryEntryV1 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]models.JobHistoryEntryV1, len(s.entries))
	for index, entry := range s.entries {
		copied := entry
		copied.Outputs = append([]models.JobOutputRecordV1(nil), entry.Outputs...)
		if copied.Outputs == nil {
			copied.Outputs = []models.JobOutputRecordV1{}
		}
		entries[index] = copied
	}

	return entries
}

// Undo moves every untouched output of a recorded job to the app trash.
// Outputs that are missing, are not regular files, or no longer match their
// recorded size and modification time are skipped and reported, never moved.
func (s *Store) Undo(jobID string) models.UndoResultV1 {
	jobID = strings.TrimSpace(jobID)

	s.mu.Lock()
	defer s.mu.Unlock()

	result := newUndoResult()
	if !s.configured() {
		result.Message = "job history is not configured"
		return result
	}
	if jobID == "" {
		result.Message = "job id is required"
		return result
	}

	index := s.findIndexLocked(jobID)
	if index < 0 {
		result.Message = fmt.Sprintf("job '%s' is not in history", jobID)
		return result
	}

	entry := &s.entries[index]
	if len(entry.Outputs) == 0 {
		result.Message = "job has no recorded outputs"
		return result
	}

	pending := 0
	for _, output := range entry.Outputs {
		if !output.Undone {
			pending++
		}
	}
	if pending == 0 {
		result.Success = true
		result.Message = "job is already undone"
		return result
	}

	for outputIndex := range entry.Outputs {
		output := &entry.Outputs[outputIndex]
		if output.Undone {
			continue
		}

		info, err := os.Stat(output.Path)
		if err != nil {
			reason := reasonMissing
			if !errors.Is(err, os.ErrNotExist) {
				reason = fmt.Sprintf("file cannot be accessed: %v", err)
			}
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{Path: output.Path, Reason: reason})
			continue
		}
		if !info.Mode().IsRegular() {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{Path: output.Path, Reason: "path is not a regular file"})
			continue
		}
		if !matchesFingerprint(info, *output) {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{Path: output.Path, Reason: reasonModified})
			continue
		}

		trashPath := s.trashPathFor(entry.JobID, outputIndex, output.Path)
		if err := os.MkdirAll(filepath.Dir(trashPath), dirPermissions); err != nil {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{
				Path:   output.Path,
				Reason: fmt.Sprintf("could not create trash folder: %v", err),
			})
			continue
		}
		if err := moveFile(output.Path, trashPath, s.rename); err != nil {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{
				Path:   output.Path,
				Reason: fmt.Sprintf("could not move file to trash: %v", err),
			})
			continue
		}

		output.TrashedPath = trashPath
		output.Undone = true
		result.Moved = append(result.Moved, output.Path)
	}

	entry.Undone = allUndone(entry.Outputs)
	if len(result.Moved) > 0 {
		entry.TrashEmptied = false
		result.Success = true
		if err := s.saveLocked(); err != nil {
			result.Success = false
			result.Message = appendMessage(undoMessage(len(result.Moved), len(entry.Outputs), len(result.Skipped)),
				fmt.Sprintf("history could not be saved: %v", err))
			return result
		}
	}

	result.Message = undoMessage(len(result.Moved), len(entry.Outputs), len(result.Skipped))
	return result
}

// Restore moves trashed outputs back to their original paths. A destination
// that already exists is never overwritten; the trashed copy stays in place
// and the conflict is reported.
func (s *Store) Restore(jobID string) models.UndoResultV1 {
	jobID = strings.TrimSpace(jobID)

	s.mu.Lock()
	defer s.mu.Unlock()

	result := newUndoResult()
	if !s.configured() {
		result.Message = "job history is not configured"
		return result
	}
	if jobID == "" {
		result.Message = "job id is required"
		return result
	}

	index := s.findIndexLocked(jobID)
	if index < 0 {
		result.Message = fmt.Sprintf("job '%s' is not in history", jobID)
		return result
	}

	entry := &s.entries[index]
	hasTrashed := false
	for _, output := range entry.Outputs {
		if strings.TrimSpace(output.TrashedPath) != "" {
			hasTrashed = true
			break
		}
	}
	if !hasTrashed {
		if entry.TrashEmptied {
			result.Message = "the trash for this job was emptied; files cannot be restored"
			return result
		}
		result.Message = "job has nothing to restore"
		return result
	}

	for outputIndex := range entry.Outputs {
		output := &entry.Outputs[outputIndex]
		trashPath := strings.TrimSpace(output.TrashedPath)
		if trashPath == "" {
			continue
		}

		if _, err := os.Stat(output.Path); err == nil {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{Path: output.Path, Reason: "destination already exists"})
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{
				Path:   output.Path,
				Reason: fmt.Sprintf("destination cannot be checked: %v", err),
			})
			continue
		}

		if _, err := os.Stat(trashPath); err != nil {
			reason := "file is missing from trash"
			if !errors.Is(err, os.ErrNotExist) {
				reason = fmt.Sprintf("trashed file cannot be accessed: %v", err)
			}
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{Path: output.Path, Reason: reason})
			continue
		}

		if err := os.MkdirAll(filepath.Dir(output.Path), dirPermissions); err != nil {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{
				Path:   output.Path,
				Reason: fmt.Sprintf("could not create destination folder: %v", err),
			})
			continue
		}
		if err := moveFile(trashPath, output.Path, s.rename); err != nil {
			result.Skipped = append(result.Skipped, models.UndoSkippedV1{
				Path:   output.Path,
				Reason: fmt.Sprintf("could not restore file: %v", err),
			})
			continue
		}

		output.TrashedPath = ""
		output.Undone = false
		result.Moved = append(result.Moved, output.Path)
	}

	entry.Undone = allUndone(entry.Outputs)
	if len(result.Moved) > 0 {
		result.Success = true
		entry.TrashEmptied = false
		if err := s.saveLocked(); err != nil {
			result.Success = false
			result.Message = appendMessage(restoreMessage(len(result.Moved), len(result.Skipped)),
				fmt.Sprintf("history could not be saved: %v", err))
			return result
		}
	}

	result.Message = restoreMessage(len(result.Moved), len(result.Skipped))
	return result
}

// EmptyTrash permanently removes the app trash directory. History entries stay
// for the audit trail: entries that had trashed files are marked TrashEmptied,
// their per-output trash paths are cleared, and the outputs stay marked undone
// because the files no longer exist.
func (s *Store) EmptyTrash() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.configured() {
		return errors.New("jobhistory: history path is not configured")
	}

	if err := os.RemoveAll(s.trashRoot); err != nil {
		return fmt.Errorf("jobhistory: empty trash: %w", err)
	}

	for index := range s.entries {
		entry := &s.entries[index]
		touched := false
		for outputIndex := range entry.Outputs {
			output := &entry.Outputs[outputIndex]
			if output.TrashedPath == "" {
				continue
			}
			output.TrashedPath = ""
			output.Undone = true
			touched = true
		}
		if touched {
			entry.TrashEmptied = true
			entry.Undone = allUndone(entry.Outputs)
		}
	}

	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("jobhistory: persist after empty trash: %w", err)
	}

	return nil
}

func (s *Store) load() {
	if !s.configured() {
		s.entries = []models.JobHistoryEntryV1{}
		return
	}

	content, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("jobhistory.load.failed path=%s err=%v", s.path, err)
		}
		s.entries = []models.JobHistoryEntryV1{}
		return
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		s.entries = []models.JobHistoryEntryV1{}
		return
	}

	entries := make([]models.JobHistoryEntryV1, 0)
	if err := json.Unmarshal(content, &entries); err != nil {
		log.Printf("jobhistory.load.malformed path=%s err=%v", s.path, err)
		s.entries = []models.JobHistoryEntryV1{}
		return
	}

	s.entries = normalizeEntries(entries)
	s.pruneLocked()
}

func normalizeEntries(entries []models.JobHistoryEntryV1) []models.JobHistoryEntryV1 {
	normalized := make([]models.JobHistoryEntryV1, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		jobID := strings.TrimSpace(entry.JobID)
		if jobID == "" {
			continue
		}
		if _, exists := seen[jobID]; exists {
			continue
		}
		seen[jobID] = struct{}{}

		entry.JobID = jobID
		if entry.Outputs == nil {
			entry.Outputs = []models.JobOutputRecordV1{}
		}
		normalized = append(normalized, entry)
	}

	return normalized
}

func (s *Store) pruneLocked() {
	if s.maxEntries > 0 && len(s.entries) > s.maxEntries {
		s.entries = s.entries[:s.maxEntries]
	}
	if s.maxOutputs <= 0 {
		return
	}

	total := 0
	for _, entry := range s.entries {
		total += len(entry.Outputs)
	}
	for total > s.maxOutputs && len(s.entries) > 1 {
		oldest := s.entries[len(s.entries)-1]
		total -= len(oldest.Outputs)
		s.entries = s.entries[:len(s.entries)-1]
	}
}

func (s *Store) saveLocked() error {
	if !s.configured() {
		return errors.New("jobhistory: history path is not configured")
	}

	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, dirPermissions); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}

	payload, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}

	temp, err := os.CreateTemp(directory, ".job_history_v1-*.tmp")
	if err != nil {
		return fmt.Errorf("create history temp file: %w", err)
	}
	tempPath := temp.Name()
	discard := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}

	if _, err := temp.Write(payload); err != nil {
		discard()
		return fmt.Errorf("write history temp file: %w", err)
	}
	if err := temp.Chmod(filePermissions); err != nil {
		discard()
		return fmt.Errorf("set history permissions: %w", err)
	}
	if err := temp.Sync(); err != nil {
		discard()
		return fmt.Errorf("sync history temp file: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close history temp file: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("replace history file: %w", err)
	}

	return nil
}

func (s *Store) findIndexLocked(jobID string) int {
	for index := range s.entries {
		if s.entries[index].JobID == jobID {
			return index
		}
	}

	return -1
}

func (s *Store) trashPathFor(jobID string, outputIndex int, originalPath string) string {
	const maxCollisionAttempts = 100

	jobDir := filepath.Join(s.trashRoot, safePathComponent(jobID))
	base := safePathComponent(filepath.Base(originalPath))
	for attempt := 0; attempt <= maxCollisionAttempts; attempt++ {
		name := fmt.Sprintf("%d-%s", outputIndex, base)
		if attempt > 0 {
			name = fmt.Sprintf("%d-%s.%d", outputIndex, base, attempt)
		}

		candidate := filepath.Join(jobDir, name)
		_, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		if err != nil {
			// The path cannot be inspected; return it so the move reports the
			// real problem instead of hiding it behind a renamed candidate.
			return candidate
		}
	}

	return filepath.Join(jobDir, fmt.Sprintf("%d-%s.%d", outputIndex, base, maxCollisionAttempts+1))
}

func safePathComponent(value string) string {
	cleaned := filepath.Base(filepath.Clean(strings.TrimSpace(value)))
	if cleaned == "" || cleaned == "." || cleaned == ".." || cleaned == string(filepath.Separator) {
		return "item"
	}

	return cleaned
}

func isTerminalStatus(status string) bool {
	switch status {
	case models.JobStatusSuccess,
		models.JobStatusPartialSuccess,
		models.JobStatusFailed,
		models.JobStatusCancelled,
		models.JobStatusInterrupted:
		return true
	default:
		return false
	}
}

func successfulOutputs(job models.JobResultV1) []string {
	outputs := make([]string, 0, len(job.Items))
	for _, item := range job.Items {
		if !item.Success {
			continue
		}
		if len(item.Outputs) > 0 {
			outputs = append(outputs, item.Outputs...)
			continue
		}
		outputs = append(outputs, item.OutputPath)
	}

	return outputs
}

// fingerprintOutputs dedupes, normalizes and stats output paths. Missing or
// non-regular paths are skipped: they cannot be undone.
func fingerprintOutputs(outputs []string) []models.JobOutputRecordV1 {
	records := make([]models.JobOutputRecordV1, 0, len(outputs))
	seen := make(map[string]struct{}, len(outputs))
	for _, output := range outputs {
		trimmed := strings.TrimSpace(output)
		if trimmed == "" {
			continue
		}
		path := filepath.Clean(trimmed)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}

		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		records = append(records, models.JobOutputRecordV1{
			Path:       path,
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime().UnixMilli(),
		})
	}

	return records
}

func matchesFingerprint(info os.FileInfo, output models.JobOutputRecordV1) bool {
	if info.Size() != output.SizeBytes {
		return false
	}
	if output.ModifiedAt <= 0 {
		return true
	}

	return info.ModTime().UnixMilli() == output.ModifiedAt
}

func allUndone(outputs []models.JobOutputRecordV1) bool {
	if len(outputs) == 0 {
		return false
	}
	for _, output := range outputs {
		if !output.Undone {
			return false
		}
	}

	return true
}

func newUndoResult() models.UndoResultV1 {
	return models.UndoResultV1{
		Moved:   []string{},
		Skipped: []models.UndoSkippedV1{},
	}
}

func undoMessage(moved, total, skipped int) string {
	switch {
	case moved == 0 && skipped == 0:
		return "no outputs were moved"
	case moved == 0:
		return fmt.Sprintf("no outputs were moved; skipped %d of %d", skipped, total)
	case skipped == 0:
		return fmt.Sprintf("moved %d %s to trash", moved, pluralize("output", moved))
	default:
		return fmt.Sprintf("moved %d of %d outputs to trash; skipped %d", moved, total, skipped)
	}
}

func restoreMessage(moved, skipped int) string {
	switch {
	case moved == 0 && skipped == 0:
		return "no outputs were restored"
	case moved == 0:
		return fmt.Sprintf("no outputs were restored; skipped %d", skipped)
	case skipped == 0:
		return fmt.Sprintf("restored %d %s", moved, pluralize("output", moved))
	default:
		return fmt.Sprintf("restored %d outputs; skipped %d", moved, skipped)
	}
}

func appendMessage(message, suffix string) string {
	if strings.TrimSpace(message) == "" {
		return suffix
	}

	return message + "; " + suffix
}

func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}

// moveFile renames src to dst and falls back to copy+remove when rename is not
// possible, which is the cross-device case on most platforms. Metadata
// (permissions, modification time) is preserved by the fallback so the
// recorded fingerprint stays valid after a round trip.
func moveFile(src, dst string, rename func(oldPath, newPath string) error) error {
	if rename != nil {
		if err := rename(src, dst); err == nil {
			return nil
		}
	}

	if err := copyFilePreservingMetadata(src, dst); err != nil {
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := os.Remove(src); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("remove source %s: %w", src, err)
	}

	return nil
}

func copyFilePreservingMetadata(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file")
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()

	permissions := info.Mode().Perm()
	if permissions == 0 {
		permissions = filePermissions
	}
	target, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, permissions)
	if err != nil {
		return err
	}

	if _, copyErr := io.Copy(target, source); copyErr != nil {
		_ = target.Close()
		_ = os.Remove(dst)
		return copyErr
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}

	modified := info.ModTime()
	if err := os.Chtimes(dst, modified, modified); err != nil {
		_ = os.Remove(dst)
		return err
	}

	return nil
}
