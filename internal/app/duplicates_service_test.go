package app

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/testutil"
)

func TestDuplicatesServiceScanLifecycle(t *testing.T) {
	bundle := newTestApp(t)

	directory := t.TempDir()
	content := bytes.Repeat([]byte("duplicates-service"), 1024)
	testutil.WriteFile(t, filepath.Join(directory, "one.bin"), content)
	testutil.WriteFile(t, filepath.Join(directory, "two.bin"), content)

	start := bundle.Duplicates.StartScan(models.DuplicateScanRequest{
		Directories:  []string{directory},
		MinSizeBytes: 1,
	})
	if !start.Success {
		t.Fatalf("start scan: %s", start.Message)
	}
	if start.ScanID == "" {
		t.Fatal("start scan returned no scan id")
	}

	var scan models.DuplicateScanState
	testutil.Eventually(t, 10*time.Second, 5*time.Millisecond, func() bool {
		status := bundle.Duplicates.GetScan(start.ScanID)
		if !status.Found || status.Scan == nil {
			return false
		}
		if status.Scan.Status == models.JobStatusQueued || status.Scan.Status == models.JobStatusRunning {
			return false
		}
		scan = *status.Scan

		return true
	})

	if scan.Status != models.JobStatusSuccess || !scan.Success {
		t.Fatalf("terminal state = %+v, want success", scan)
	}
	if len(scan.Groups) != 1 || scan.Groups[0].Count != 2 {
		t.Fatalf("groups = %+v, want one group of 2", scan.Groups)
	}

	list := bundle.Duplicates.ListScans()
	if !list.Success || len(list.Scans) == 0 {
		t.Fatalf("list scans = %+v, want the finished scan", list)
	}

	unknown := bundle.Duplicates.GetScan("does-not-exist")
	if unknown.Success || unknown.Found {
		t.Fatalf("get unknown scan = %+v, want not found", unknown)
	}
	if cancel := bundle.Duplicates.CancelScan("does-not-exist"); cancel.Success {
		t.Fatalf("cancel unknown scan = %+v, want failure", cancel)
	}
}

func TestDuplicatesServiceRejectsEmptyRequest(t *testing.T) {
	bundle := newTestApp(t)

	response := bundle.Duplicates.StartScan(models.DuplicateScanRequest{})
	if response.Success {
		t.Fatalf("start scan without folders = %+v, want failure", response)
	}
}
