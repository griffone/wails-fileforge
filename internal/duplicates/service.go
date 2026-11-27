package duplicates

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/eventbus"
)

const (
	// ProgressEventName is emitted while a scan walks and hashes.
	ProgressEventName = "duplicates/progress"
	// StateEventName carries the full scan snapshot on submission and on every
	// terminal status.
	StateEventName = "duplicates/state"

	// DefaultMaxScans bounds how many scan snapshots are retained in memory.
	// Each snapshot keeps the paths of its duplicate groups, so retaining every
	// historical scan would grow without bound.
	DefaultMaxScans = 5

	scanKind = "duplicates"
)

// Config configures one scanner service instance.
type Config struct {
	// Bus delivers progress and state events. A nil bus drops them, which keeps
	// headless tests and early startup safe.
	Bus eventbus.Bus
	// DefaultWorkers is the hashing concurrency for requests that do not choose
	// one. Zero uses DefaultWorkers.
	DefaultWorkers int
	// DefaultMaxFiles is the file cap for requests that do not choose one. Zero
	// uses DefaultMaxFiles.
	DefaultMaxFiles int
	// MaxScans bounds retained scans. Zero uses DefaultMaxScans.
	MaxScans int
}

// Service owns the scan registry, cancellation and event emission. The scan
// algorithm itself lives in Scan and is stateless.
type Service struct {
	bus             eventbus.Bus
	defaultWorkers  int
	defaultMaxFiles int
	maxScans        int

	mu     sync.Mutex
	scans  map[string]*scanRecord
	closed bool
	wg     sync.WaitGroup
}

type scanRecord struct {
	state      models.DuplicateScanState
	cancel     context.CancelFunc
	cancelling bool
}

// NewService builds a scanner service.
func NewService(cfg Config) *Service {
	if cfg.DefaultWorkers <= 0 {
		cfg.DefaultWorkers = DefaultWorkers
	}
	if cfg.DefaultMaxFiles <= 0 {
		cfg.DefaultMaxFiles = DefaultMaxFiles
	}
	if cfg.MaxScans <= 0 {
		cfg.MaxScans = DefaultMaxScans
	}

	return &Service{
		bus:             cfg.Bus,
		defaultWorkers:  cfg.DefaultWorkers,
		defaultMaxFiles: cfg.DefaultMaxFiles,
		maxScans:        cfg.MaxScans,
		scans:           make(map[string]*scanRecord),
	}
}

// StartScan validates the request, registers a scan and returns its id. The
// scan runs on the parent context so a bound-method context that Wails cancels
// on RPC return cannot kill it.
func (s *Service) StartScan(parent context.Context, request models.DuplicateScanRequest) models.DuplicateStartResponse {
	if s == nil {
		return models.DuplicateStartResponse{Success: false, Message: "the duplicate scanner is not available"}
	}
	if err := ValidateRequest(request); err != nil {
		return models.DuplicateStartResponse{Success: false, Message: err.Error()}
	}
	if parent == nil {
		parent = context.Background()
	}

	request = s.applyDefaults(request)

	scanID, err := newScanID()
	if err != nil {
		return models.DuplicateStartResponse{Success: false, Message: err.Error()}
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()

		return models.DuplicateStartResponse{Success: false, Message: "the duplicate scanner is shutting down"}
	}
	if s.maxScans > 0 && len(s.scans) >= s.maxScans {
		s.evictTerminalLocked(len(s.scans) - s.maxScans + 1)
		if len(s.scans) >= s.maxScans {
			s.mu.Unlock()

			return models.DuplicateStartResponse{Success: false, Message: "too many scans are running; wait for one to finish"}
		}
	}

	ctx, cancel := context.WithCancel(parent)
	now := time.Now().UnixMilli()
	record := &scanRecord{
		state: models.DuplicateScanState{
			ScanID:    scanID,
			Kind:      scanKind,
			Status:    models.JobStatusQueued,
			Progress:  models.JobProgress{Stage: StageWalking, Message: "Queued"},
			Groups:    []models.DuplicateScanGroup{},
			Warnings:  []models.DuplicateScanWarning{},
			StartedAt: now,
		},
		cancel: cancel,
	}
	s.scans[scanID] = record
	s.wg.Add(1)
	s.mu.Unlock()

	s.emitState(scanID)
	go func() {
		defer s.wg.Done()
		s.run(ctx, scanID, request)
	}()

	return models.DuplicateStartResponse{Success: true, Message: "scan started", ScanID: scanID}
}

// applyDefaults fills the request with the service defaults. A negative
// MaxFiles still means "unlimited", so only zero is replaced.
func (s *Service) applyDefaults(request models.DuplicateScanRequest) models.DuplicateScanRequest {
	if request.Workers <= 0 {
		request.Workers = s.defaultWorkers
	}
	if request.MaxFiles == 0 {
		request.MaxFiles = s.defaultMaxFiles
	}

	return request
}

// GetScan returns one scan snapshot.
func (s *Service) GetScan(scanID string) models.DuplicateScanStatusResponse {
	if s == nil {
		return models.DuplicateScanStatusResponse{Success: false, Message: "the duplicate scanner is not available"}
	}

	trimmed := strings.TrimSpace(scanID)
	if trimmed == "" {
		return models.DuplicateScanStatusResponse{Success: false, Message: "a scan id is required"}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	record, found := s.scans[trimmed]
	if !found {
		return models.DuplicateScanStatusResponse{Success: false, Message: "scan not found", Found: false}
	}

	state := cloneScanState(record.state)

	return models.DuplicateScanStatusResponse{Success: true, Message: "scan found", Found: true, Scan: &state}
}

// CancelScan asks a running scan to stop. It returns as soon as the context is
// cancelled; the scan itself reports the terminal cancelled status.
func (s *Service) CancelScan(scanID string) models.DuplicateScanActionResponse {
	if s == nil {
		return models.DuplicateScanActionResponse{Success: false, Message: "the duplicate scanner is not available"}
	}

	trimmed := strings.TrimSpace(scanID)
	if trimmed == "" {
		return models.DuplicateScanActionResponse{Success: false, Message: "a scan id is required"}
	}

	s.mu.Lock()
	record, found := s.scans[trimmed]
	if !found {
		s.mu.Unlock()

		return models.DuplicateScanActionResponse{Success: false, Message: "scan not found", ScanID: trimmed}
	}
	if isTerminalStatus(record.state.Status) {
		s.mu.Unlock()

		return models.DuplicateScanActionResponse{Success: false, Message: "the scan already finished", ScanID: trimmed}
	}

	if record.cancelling {
		s.mu.Unlock()

		return models.DuplicateScanActionResponse{Success: true, Message: "cancellation already requested", ScanID: trimmed}
	}
	record.cancelling = true
	cancel := record.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	return models.DuplicateScanActionResponse{Success: true, Message: "cancellation requested", ScanID: trimmed}
}

// ListScans returns every retained scan, newest first.
func (s *Service) ListScans() models.DuplicateScanListResponse {
	if s == nil {
		return models.DuplicateScanListResponse{Success: false, Message: "the duplicate scanner is not available", Scans: []models.DuplicateScanState{}}
	}

	s.mu.Lock()
	scans := make([]models.DuplicateScanState, 0, len(s.scans))
	for _, record := range s.scans {
		scans = append(scans, cloneScanState(record.state))
	}
	s.mu.Unlock()

	sort.Slice(scans, func(i, j int) bool {
		if scans[i].StartedAt != scans[j].StartedAt {
			return scans[i].StartedAt > scans[j].StartedAt
		}

		return scans[i].ScanID > scans[j].ScanID
	})

	return models.DuplicateScanListResponse{Success: true, Message: "scans listed", Scans: scans}
}

// Shutdown cancels every running scan and waits for its goroutine to exit.
func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()

		return nil
	}
	s.closed = true
	cancels := make([]context.CancelFunc, 0, len(s.scans))
	for _, record := range s.scans {
		if record.cancel != nil {
			cancels = append(cancels, record.cancel)
		}
	}
	running := len(s.scans)
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	if running == 0 {
		return nil
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run executes one scan and publishes its terminal snapshot.
func (s *Service) run(ctx context.Context, scanID string, request models.DuplicateScanRequest) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("duplicates.scan.panic scanId=%s err=%v", scanID, recovered)
			s.complete(scanID, Result{}, fmt.Errorf("the scan stopped unexpectedly: %v", recovered))
		}
	}()

	result, err := Scan(ctx, request, func(progress models.JobProgress) {
		s.reportProgress(scanID, progress)
	})
	s.complete(scanID, result, err)
}

// reportProgress updates the live snapshot and emits a progress event.
func (s *Service) reportProgress(scanID string, progress models.JobProgress) {
	s.mu.Lock()
	record, found := s.scans[scanID]
	if !found || isTerminalStatus(record.state.Status) {
		s.mu.Unlock()

		return
	}
	record.state.Status = models.JobStatusRunning
	record.state.Progress = progress
	s.mu.Unlock()

	s.emit(ProgressEventName, models.DuplicateScanProgressEvent{
		ScanID:   scanID,
		Kind:     scanKind,
		Status:   models.JobStatusRunning,
		Progress: progress,
	})
}

// complete stores the terminal snapshot and emits it. Cancellation is reported
// as cancelled, a request problem as failed, and everything else as success
// even when warnings were collected.
func (s *Service) complete(scanID string, result Result, scanErr error) {
	s.mu.Lock()
	record, found := s.scans[scanID]
	if !found {
		s.mu.Unlock()

		return
	}

	status := models.JobStatusSuccess
	success := true
	message := resultMessage(result.Totals)
	switch {
	case errors.Is(scanErr, context.Canceled), errors.Is(scanErr, context.DeadlineExceeded):
		status = models.JobStatusCancelled
		success = false
		message = "scan cancelled"
	case scanErr != nil:
		status = models.JobStatusFailed
		success = false
		message = scanErr.Error()
	case len(result.Warnings) > 0:
		message = fmt.Sprintf("%s; %d %s", message, len(result.Warnings), pluralize("warning", len(result.Warnings)))
	}

	record.state.Status = status
	record.state.Success = success
	record.state.Message = message
	record.state.Groups = result.Groups
	record.state.Totals = result.Totals
	record.state.Warnings = result.Warnings
	record.state.Progress = models.JobProgress{
		Current: result.Totals.FilesScanned,
		Total:   result.Totals.FilesScanned,
		Stage:   StageDone,
		Message: message,
	}
	record.state.EndedAt = time.Now().UnixMilli()
	release := record.cancel
	record.cancel = nil
	if record.state.Groups == nil {
		record.state.Groups = []models.DuplicateScanGroup{}
	}
	if record.state.Warnings == nil {
		record.state.Warnings = []models.DuplicateScanWarning{}
	}
	s.mu.Unlock()

	// The scan is done; release its context instead of waiting for the parent.
	if release != nil {
		release()
	}

	s.emitState(scanID)
}

// emitState publishes the current snapshot of one scan.
func (s *Service) emitState(scanID string) {
	s.mu.Lock()
	record, found := s.scans[scanID]
	if !found {
		s.mu.Unlock()

		return
	}
	state := cloneScanState(record.state)
	s.mu.Unlock()

	s.emit(StateEventName, state)
}

// evictTerminalLocked drops the oldest terminal scans until want have been
// removed or only running scans remain. The caller must hold s.mu.
func (s *Service) evictTerminalLocked(want int) {
	if want <= 0 {
		return
	}

	type terminalScan struct {
		id        string
		endedAt   int64
		startedAt int64
	}
	terminal := make([]terminalScan, 0, len(s.scans))
	for id, record := range s.scans {
		if !isTerminalStatus(record.state.Status) {
			continue
		}
		terminal = append(terminal, terminalScan{id: id, endedAt: record.state.EndedAt, startedAt: record.state.StartedAt})
	}
	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].endedAt != terminal[j].endedAt {
			return terminal[i].endedAt < terminal[j].endedAt
		}
		if terminal[i].startedAt != terminal[j].startedAt {
			return terminal[i].startedAt < terminal[j].startedAt
		}

		return terminal[i].id < terminal[j].id
	})

	for index := 0; index < len(terminal) && index < want; index++ {
		delete(s.scans, terminal[index].id)
	}
}

// emit forwards one event to the configured bus.
func (s *Service) emit(name string, payload any) {
	if s.bus == nil {
		return
	}

	s.bus.Emit(name, payload)
}

func newScanID() (string, error) {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "", fmt.Errorf("duplicates: generate scan id: %w", err)
	}

	return hex.EncodeToString(buffer[:]), nil
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

func cloneScanState(state models.DuplicateScanState) models.DuplicateScanState {
	copied := state
	copied.Groups = make([]models.DuplicateScanGroup, len(state.Groups))
	for index, group := range state.Groups {
		group.Paths = append([]string(nil), group.Paths...)
		if group.Paths == nil {
			group.Paths = []string{}
		}
		copied.Groups[index] = group
	}
	copied.Warnings = append([]models.DuplicateScanWarning(nil), state.Warnings...)
	if copied.Groups == nil {
		copied.Groups = []models.DuplicateScanGroup{}
	}
	if copied.Warnings == nil {
		copied.Warnings = []models.DuplicateScanWarning{}
	}

	return copied
}
