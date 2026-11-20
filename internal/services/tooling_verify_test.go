package services

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeVerifyTestPNG(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create output directory: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 120, A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png: %v", err)
	}
}

// TestVerifyJobReadsHistoryWhenEngineLosesTheJob proves a recorded job stays
// verifiable after the engine no longer holds its snapshot.
func TestVerifyJobReadsHistoryWhenEngineLosesTheJob(t *testing.T) {
	service, _ := newHistoryTestService(t, &historyFakeTool{id: "tool.fake.verify"})
	output := filepath.Join(t.TempDir(), "converted.png")
	writeVerifyTestPNG(t, output)

	history := service.JobHistory()
	history.RecordOutputs("history_job_1", "tool.fake.verify", []string{output})

	report := service.VerifyJob("history_job_1")
	if !report.Success {
		t.Fatalf("expected the history-only job to verify, got %s (%v)", report.Message, report.Warnings)
	}
	if len(report.Outputs) != 1 || report.Outputs[0].Path != output {
		t.Fatalf("expected the recorded output to be verified, got %+v", report.Outputs)
	}

	history.RecordOutputs("history_job_2", "tool.fake.verify", []string{output})
	undo := service.UndoJob("history_job_2")
	if !undo.Success {
		t.Fatalf("expected undo success, got %+v", undo)
	}

	report = service.VerifyJob("history_job_2")
	if report.Success || report.Message != "job has no outputs to verify" {
		t.Fatalf("expected a trashed output to be excluded, got %+v", report)
	}

	missing := service.VerifyJob("job_not_recorded")
	if missing.Success || !strings.Contains(missing.Message, "not found") {
		t.Fatalf("expected an unknown job to be reported, got %+v", missing)
	}
}
