package duplicates

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/testutil"
)

type capturedEvent struct {
	name    string
	payload any
}

type eventSink struct {
	mu     sync.Mutex
	events []capturedEvent
}

func (s *eventSink) Emit(name string, payload any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, capturedEvent{name: name, payload: payload})
}

func (s *eventSink) snapshot() []capturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]capturedEvent(nil), s.events...)
}

func waitForTerminal(t *testing.T, service *Service, scanID string) models.DuplicateScanState {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		response := service.GetScan(scanID)
		if !response.Found || response.Scan == nil {
			t.Fatalf("scan %s was not found: %s", scanID, response.Message)
		}
		if isTerminalStatus(response.Scan.Status) {
			return *response.Scan
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan %s did not finish; last status %q", scanID, response.Scan.Status)
		}

		time.Sleep(2 * time.Millisecond)
	}
}

func TestServiceEmitsProgressAndTerminalState(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("event"), 2048)
	testutil.WriteFile(t, filepath.Join(directory, "first.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, "second.bin"), content)

	sink := &eventSink{}
	service := NewService(Config{Bus: sink})
	t.Cleanup(func() {
		_ = service.Shutdown(context.Background())
	})

	response := service.StartScan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	})
	if !response.Success {
		t.Fatalf("start scan: %s", response.Message)
	}

	state := waitForTerminal(t, service, response.ScanID)
	if state.Status != models.JobStatusSuccess || !state.Success {
		t.Fatalf("terminal state = %+v, want success", state)
	}
	if len(state.Groups) != 1 || state.Groups[0].Count != 2 {
		t.Fatalf("groups = %+v, want one group of 2", state.Groups)
	}

	stages := map[string]bool{}
	queuedState := false
	terminalState := false
	for _, event := range sink.snapshot() {
		switch event.name {
		case ProgressEventName:
			progress, ok := event.payload.(models.DuplicateScanProgressEvent)
			if !ok {
				t.Fatalf("progress payload type = %T", event.payload)
			}
			if progress.ScanID != response.ScanID {
				t.Fatalf("progress scan id = %q, want %q", progress.ScanID, response.ScanID)
			}
			stages[progress.Progress.Stage] = true
		case StateEventName:
			snapshot, ok := event.payload.(models.DuplicateScanState)
			if !ok {
				t.Fatalf("state payload type = %T", event.payload)
			}
			if snapshot.ScanID != response.ScanID {
				t.Fatalf("state scan id = %q, want %q", snapshot.ScanID, response.ScanID)
			}
			if snapshot.Status == models.JobStatusQueued {
				queuedState = true
			}
			if snapshot.Status == models.JobStatusSuccess {
				terminalState = true
			}
		}
	}

	if !queuedState {
		t.Fatal("expected a queued state event")
	}
	if !terminalState {
		t.Fatal("expected a terminal state event")
	}
	if !stages[StageWalking] || !stages[StageHashing] {
		t.Fatalf("progress stages = %v, want walking and hashing", stages)
	}
}

func TestServiceCancelScanStopsPromptly(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("cancel"), 8*1024)
	for index := 0; index < 1000; index++ {
		testutil.WriteFile(t, filepath.Join(directory, fmt.Sprintf("file-%04d.bin", index)), content)
	}

	service := NewService(Config{})
	t.Cleanup(func() {
		_ = service.Shutdown(context.Background())
	})

	response := service.StartScan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	})
	if !response.Success {
		t.Fatalf("start scan: %s", response.Message)
	}

	cancelled := service.CancelScan(response.ScanID)
	if !cancelled.Success {
		t.Fatalf("cancel scan: %s", cancelled.Message)
	}

	state := waitForTerminal(t, service, response.ScanID)
	if state.Status != models.JobStatusCancelled {
		t.Fatalf("terminal status = %q, want %q", state.Status, models.JobStatusCancelled)
	}
	if state.Success {
		t.Fatal("a cancelled scan must not report success")
	}
}

func TestServiceShutdownStopsRunningScans(t *testing.T) {
	directory := t.TempDir()
	content := bytes.Repeat([]byte("shutdown"), 8*1024)
	for index := 0; index < 400; index++ {
		testutil.WriteFile(t, filepath.Join(directory, fmt.Sprintf("file-%04d.bin", index)), content)
	}

	service := NewService(Config{})
	response := service.StartScan(context.Background(), models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	})
	if !response.Success {
		t.Fatalf("start scan: %s", response.Message)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	state := service.GetScan(response.ScanID)
	if !state.Found || state.Scan == nil {
		t.Fatal("scan snapshot disappeared during shutdown")
	}
	if !isTerminalStatus(state.Scan.Status) {
		t.Fatalf("status after shutdown = %q, want terminal", state.Scan.Status)
	}

	after := service.StartScan(context.Background(), models.DuplicateScanRequest{Directories: []string{directory}})
	if after.Success {
		t.Fatal("expected a scan start to be rejected after shutdown")
	}
}

func TestServiceCapEvictsOldestTerminalScan(t *testing.T) {
	service := NewService(Config{MaxScans: 2})
	t.Cleanup(func() {
		_ = service.Shutdown(context.Background())
	})

	directory := t.TempDir()
	first := service.StartScan(context.Background(), models.DuplicateScanRequest{Directories: []string{directory}})
	waitForTerminal(t, service, first.ScanID)

	second := service.StartScan(context.Background(), models.DuplicateScanRequest{Directories: []string{directory}})
	waitForTerminal(t, service, second.ScanID)

	third := service.StartScan(context.Background(), models.DuplicateScanRequest{Directories: []string{directory}})
	if !third.Success {
		t.Fatalf("start third scan: %s", third.Message)
	}
	waitForTerminal(t, service, third.ScanID)

	list := service.ListScans()
	if !list.Success {
		t.Fatalf("list scans: %s", list.Message)
	}
	if len(list.Scans) != 2 {
		t.Fatalf("retained scans = %d, want 2", len(list.Scans))
	}

	retained := make(map[string]bool, len(list.Scans))
	for _, state := range list.Scans {
		retained[state.ScanID] = true
	}
	if retained[first.ScanID] {
		t.Fatalf("oldest scan %s was not evicted", first.ScanID)
	}
	if !retained[second.ScanID] || !retained[third.ScanID] {
		t.Fatalf("retained scans = %v, want the two newest", retained)
	}
}

func TestServiceRejectsInvalidRequestsAndUnknownScans(t *testing.T) {
	service := NewService(Config{})
	t.Cleanup(func() {
		_ = service.Shutdown(context.Background())
	})

	if response := service.StartScan(context.Background(), models.DuplicateScanRequest{}); response.Success {
		t.Fatal("expected an empty request to be rejected")
	}

	missing := filepath.Join(t.TempDir(), "missing")
	if response := service.StartScan(context.Background(), models.DuplicateScanRequest{Directories: []string{missing}}); response.Success {
		t.Fatal("expected a missing folder to be rejected")
	}

	if response := service.GetScan("nope"); response.Success || response.Found {
		t.Fatalf("get unknown scan = %+v, want not found", response)
	}
	if response := service.CancelScan("nope"); response.Success {
		t.Fatalf("cancel unknown scan = %+v, want failure", response)
	}
	if response := service.GetScan("  "); response.Success {
		t.Fatalf("get blank scan id = %+v, want failure", response)
	}
}
