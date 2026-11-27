package services

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
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

// runVerifyTargetJob submits one fake job with the given options and waits for
// it to reach a terminal state.
func runVerifyTargetJob(t *testing.T, service *ToolingService, toolID string, options map[string]any) string {
	t.Helper()

	submit := service.RunJob(models.JobRequest{
		ToolID:     toolID,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-verify-input.txt"},
		Options:    options,
	})
	if !submit.Success {
		t.Fatalf("expected the job to be submitted, got %+v", submit)
	}
	waitForTerminalStatus(t, service, submit.JobID)

	return submit.JobID
}

func targetSizeCheck(output models.VerifiedOutput) (models.VerificationCheck, bool) {
	for _, check := range output.Checks {
		if check.Key == "target_size" {
			return check, true
		}
	}

	return models.VerificationCheck{}, false
}

func TestVerifyJobReportsRequestedTargetComparison(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "converted.txt")
	tool := &historyFakeTool{id: "tool.fake.target.met", outputPath: output}
	service, _ := newHistoryTestService(t, tool)

	target := int64(1024 * 1024)
	jobID := runVerifyTargetJob(t, service, tool.id, map[string]any{
		"targetSizeBytes": float64(target),
	})

	report := service.VerifyJob(jobID)
	if !report.Success {
		t.Fatalf("expected a met target to verify, got %s (%v)", report.Message, report.Warnings)
	}
	if len(report.Outputs) != 1 {
		t.Fatalf("expected one output, got %+v", report.Outputs)
	}

	verified := report.Outputs[0]
	if verified.ActualBytes != int64(len("converted")) || verified.TargetBytes != target {
		t.Fatalf("expected actual/target bytes, got actual=%d target=%d", verified.ActualBytes, verified.TargetBytes)
	}
	check, found := targetSizeCheck(verified)
	if !found {
		t.Fatalf("expected a target_size check, got %+v", verified.Checks)
	}
	if !check.Passed || !check.Advisory || check.Value != "9 B / 1.0 MB" {
		t.Fatalf("expected a passing advisory 9 B / 1.0 MB target check, got %+v", check)
	}
}

func TestVerifyJobReportsAnUnmetTargetAsAdvisory(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "converted.txt")
	tool := &historyFakeTool{id: "tool.fake.target.missed", outputPath: output}
	service, _ := newHistoryTestService(t, tool)

	jobID := runVerifyTargetJob(t, service, tool.id, map[string]any{
		"targetSizeBytes": 1,
	})

	report := service.VerifyJob(jobID)
	if !report.Success {
		t.Fatalf("expected an unmet target to stay verified, got %+v", report)
	}
	if report.Message != "verified 1 output with 1 advisory warning" {
		t.Fatalf("expected an honest advisory message, got %q", report.Message)
	}
	check, found := targetSizeCheck(report.Outputs[0])
	if !found {
		t.Fatalf("expected a target_size check, got %+v", report.Outputs[0].Checks)
	}
	if check.Passed {
		t.Fatalf("expected the target comparison to fail, got %+v", check)
	}
	if !check.Advisory {
		t.Fatalf("expected the target check to be advisory, got %+v", check)
	}
	if !strings.Contains(check.Detail, "output is 9 B") || !strings.Contains(check.Detail, "target is 1 B") {
		t.Fatalf("expected an actionable size detail, got %q", check.Detail)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "exceeds the 1 B target") {
		t.Fatalf("expected a target warning, got %v", report.Warnings)
	}
}

func TestVerifyJobWithoutTargetKeepsTargetFieldsEmpty(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "converted.txt")
	tool := &historyFakeTool{id: "tool.fake.target.none", outputPath: output}
	service, _ := newHistoryTestService(t, tool)

	jobID := runVerifyTargetJob(t, service, tool.id, nil)

	report := service.VerifyJob(jobID)
	if !report.Success {
		t.Fatalf("expected the output to verify, got %s (%v)", report.Message, report.Warnings)
	}
	verified := report.Outputs[0]
	if verified.ActualBytes != 0 || verified.TargetBytes != 0 {
		t.Fatalf("expected no target fields without a target, got actual=%d target=%d", verified.ActualBytes, verified.TargetBytes)
	}
	if _, found := targetSizeCheck(verified); found {
		t.Fatalf("expected no target_size check, got %+v", verified.Checks)
	}
}

func TestVerifyJobParsesTolerantTargetOptionShapes(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "converted.txt")
	tool := &historyFakeTool{id: "tool.fake.target.shapes", outputPath: output}
	service, _ := newHistoryTestService(t, tool)

	const target = int64(2048)
	cases := []struct {
		name  string
		value any
	}{
		{name: "float64", value: float64(target)},
		{name: "int", value: int(target)},
		{name: "int64", value: target},
		{name: "numeric string", value: "2048"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			jobID := runVerifyTargetJob(t, service, tool.id, map[string]any{
				"targetSizeBytes": testCase.value,
			})

			report := service.VerifyJob(jobID)
			if len(report.Outputs) != 1 {
				t.Fatalf("expected one output, got %+v", report.Outputs)
			}
			if report.Outputs[0].TargetBytes != target {
				t.Fatalf("expected target %d, got %d", target, report.Outputs[0].TargetBytes)
			}
			if _, found := targetSizeCheck(report.Outputs[0]); !found {
				t.Fatalf("expected a target_size check, got %+v", report.Outputs[0].Checks)
			}
		})
	}
}

// TestVerifyJobRestoresTargetFromHistoryAfterRestart proves the target travels
// with the persisted history entry, so a job that left the engine can still be
// compared against its original target.
func TestVerifyJobRestoresTargetFromHistoryAfterRestart(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "converted.txt")
	tool := &historyFakeTool{id: "tool.fake.target.restart", outputPath: output}
	service, dir := newHistoryTestService(t, tool)

	target := int64(4096)
	jobID := runVerifyTargetJob(t, service, tool.id, map[string]any{
		"targetSizeBytes": target,
	})

	entries := waitForHistoryEntries(t, service, 1)
	if entries[0].TargetBytes != target {
		t.Fatalf("expected the history entry to persist target %d, got %+v", target, entries[0])
	}

	restarted := newToolingServiceWithBus(
		registry.NewRegistry(),
		jobhistory.NewStore(filepath.Join(dir, "history", "job_history.json")),
		nil,
	)
	report := restarted.VerifyJob(jobID)
	if !report.Success {
		t.Fatalf("expected the restarted service to verify from history, got %s (%v)", report.Message, report.Warnings)
	}
	verified := report.Outputs[0]
	if verified.TargetBytes != target || verified.ActualBytes != int64(len("converted")) {
		t.Fatalf("expected restored target bytes, got actual=%d target=%d", verified.ActualBytes, verified.TargetBytes)
	}
	if _, found := targetSizeCheck(verified); !found {
		t.Fatalf("expected a target_size check after restart, got %+v", verified.Checks)
	}
}
