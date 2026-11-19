package services

import (
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/registry"
)

func TestListJobsV1ReturnsEmptySliceForNewService(t *testing.T) {
	history := jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history_v1.json"))
	service := newToolingService(registry.NewRegistry(), history)

	response := service.ListJobsV1(0)
	if !response.Success {
		t.Fatalf("expected a successful response, got %+v", response)
	}
	if response.Message != "jobs listed" {
		t.Fatalf("expected the list message, got %q", response.Message)
	}
	if response.Jobs == nil {
		t.Fatal("expected an empty non-nil jobs slice")
	}
	if len(response.Jobs) != 0 {
		t.Fatalf("expected no jobs, got %+v", response.Jobs)
	}
}
