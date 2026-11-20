package services

import (
	"testing"

	"fileforge-desktop/internal/models"
)

func TestJobOutputPathsPrefersOutputsAndFallsBackToOutputPath(t *testing.T) {
	job := models.JobResult{
		Items: []models.JobResultItem{
			{Outputs: []string{"/tmp/a.pdf", "/tmp/b.pdf"}, OutputPath: "/tmp/ignored.pdf"},
			{OutputPath: "/tmp/c.pdf"},
			{Outputs: []string{"  /tmp/a.pdf  "}, OutputPath: "/tmp/dup.pdf"},
			{OutputPath: "   "},
		},
	}

	paths := jobOutputPaths(job)
	want := []string{"/tmp/a.pdf", "/tmp/b.pdf", "/tmp/c.pdf"}
	if len(paths) != len(want) {
		t.Fatalf("expected %v, got %v", want, paths)
	}
	for index := range want {
		if paths[index] != want[index] {
			t.Fatalf("expected %v, got %v", want, paths)
		}
	}
}

func TestJobOutputPathsEmptyJob(t *testing.T) {
	if paths := jobOutputPaths(models.JobResult{}); len(paths) != 0 {
		t.Fatalf("expected no paths, got %v", paths)
	}
}
