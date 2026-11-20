package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

// BenchmarkPersistJobsSnapshot records the cost of the full-snapshot
// persistence path with a realistic terminal history.
func BenchmarkPersistJobsSnapshot(b *testing.B) {
	persisted := benchmarkPersistedJobs()
	path := filepath.Join(b.TempDir(), "jobs.json")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := saveJobsSnapshot(path, persisted); err != nil {
			b.Fatalf("save jobs snapshot: %v", err)
		}
	}
}

func TestSaveJobsSnapshotWritesValidJSON(t *testing.T) {
	persisted := benchmarkPersistedJobs()
	path := filepath.Join(t.TempDir(), "jobs.json")

	if err := saveJobsSnapshot(path, persisted); err != nil {
		t.Fatalf("save jobs snapshot: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted jobs: %v", err)
	}

	var decoded []models.JobResult
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatalf("decode persisted jobs: %v", err)
	}
	if len(decoded) != len(persisted) {
		t.Fatalf("persisted jobs = %d, want %d", len(decoded), len(persisted))
	}
	if decoded[0].JobID != persisted[0].JobID {
		t.Fatalf("first persisted job = %q, want %q", decoded[0].JobID, persisted[0].JobID)
	}
}

// benchmarkPersistedJobs builds 25 terminal job snapshots with one completed
// item each, the shape written by the engine snapshot.
func benchmarkPersistedJobs() []models.JobResult {
	started := time.Now().Add(-time.Hour).UnixMilli()
	ended := time.Now().UnixMilli()

	jobs := make([]models.JobResult, 0, 25)
	for i := 0; i < 25; i++ {
		input := fmt.Sprintf("/tmp/fileforge/input_%02d.png", i)
		output := fmt.Sprintf("/tmp/fileforge/output_%02d.jpg", i)
		jobs = append(jobs, models.JobResult{
			JobID:   fmt.Sprintf("job-%02d", i),
			Success: true,
			Message: "Job completed successfully",
			ToolID:  "tool.image.convert",
			Status:  "succeeded",
			Progress: models.JobProgress{
				Current: 1,
				Total:   1,
				Stage:   "completed",
				Message: "Converted 1 image",
			},
			Items: []models.JobResultItem{
				{
					InputPath:   input,
					OutputPath:  output,
					Outputs:     []string{output},
					OutputCount: 1,
					Success:     true,
					Message:     "converted",
				},
			},
			StartedAt: started,
			EndedAt:   ended,
		})
	}

	return jobs
}
