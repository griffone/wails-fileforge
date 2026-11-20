package video

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/video/engine"
)

func newTrimPlanTool(t *testing.T) *TrimTool {
	t.Helper()
	stubVideoDuration(t, 0, false)
	return NewTrimToolWithDeps(stubRuntimeProbe{}, nil)
}

func trimOptions(startTime, endTime float64) map[string]any {
	return map[string]any{"startTime": startTime, "endTime": endTime}
}

func TestTrimPlanSingleAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	plan, planErr := newTrimPlanTool(t).Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(1, 5)))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected success plan")
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "clip_trimmed.mp4"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Adjusted || output.Exists {
		t.Fatalf("expected a fresh natural output, got %+v", output)
	}
}

func TestTrimPlanBatchWithoutOutputDirPlansPerInputOutputs(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.mp4")
	second := filepath.Join(root, "two", "second.mkv")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	plan, planErr := newTrimPlanTool(t).Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "batch", []string{first, second}, trimOptions(0, 3)))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(root, "one", "first_trimmed.mp4"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(root, "two", "second_trimmed.mp4"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
}

func TestTrimPlanUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	req := videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(2, 4))
	req.OutputDir = outDir
	plan, planErr := newTrimPlanTool(t).Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(outDir, "clip_trimmed.mp4"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestTrimPlanDefaultsOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	plan, planErr := newTrimPlanTool(t).Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(1, 5)))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["targetFormat"]; got != "mp4" {
		t.Fatalf("expected default targetFormat mp4, got %v", got)
	}
	if got := plan.EffectiveOptions["qualityPreset"]; got != "medium" {
		t.Fatalf("expected default qualityPreset medium, got %v", got)
	}
	if got := plan.EffectiveOptions["trimMode"]; got != engine.TrimModeAuto {
		t.Fatalf("expected default trimMode auto, got %v", got)
	}
}

func TestTrimPlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	writeVideoFixture(t, filepath.Join(dir, "clip_trimmed.mp4"))

	plan, planErr := newTrimPlanTool(t).Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(1, 5)))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "clip_trimmed-2.mp4"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestTrimPlanInvalidTimeRange(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	cases := []struct {
		name    string
		options map[string]any
		code    string
	}{
		{name: "end equals start", options: trimOptions(2, 2), code: engine.ErrorCodeVideoTrimInvalidTimeRange},
		{name: "end before start", options: trimOptions(4, 2), code: engine.ErrorCodeVideoTrimInvalidTimeRange},
		{name: "negative start", options: trimOptions(-1, 2), code: engine.ErrorCodeVideoTrimInvalidTimeRange},
		{name: "missing start", options: map[string]any{"endTime": float64(2)}, code: engine.ErrorCodeVideoTrimValidation},
		{name: "missing end", options: map[string]any{"startTime": float64(1)}, code: engine.ErrorCodeVideoTrimValidation},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, planErr := newTrimPlanTool(t).Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, testCase.options))
			if planErr == nil {
				t.Fatal("expected a time range error")
			}
			if planErr.Code != testCase.code {
				t.Fatalf("expected %q, got %q", testCase.code, planErr.Code)
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
		})
	}
}

func TestTrimPlanExplicitOutputConflict(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	outputPath := filepath.Join(dir, "custom.mp4")
	writeVideoFixture(t, input)
	writeVideoFixture(t, outputPath)

	options := trimOptions(1, 5)
	options["outputPath"] = outputPath

	plan, planErr := newTrimPlanTool(t).Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, options))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.Code != engine.ErrorCodeVideoTrimOutputExists {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoTrimOutputExists, planErr.Code)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
	if len(plan.Outputs) != 1 || !plan.Outputs[0].Exists {
		t.Fatal("expected the conflicting output to be reported as existing")
	}
}

func TestTrimValidateBatchWithoutOutputDir(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.mp4")
	second := filepath.Join(root, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	validationErr := newTrimPlanTool(t).Validate(context.Background(), videoToolRequest(ToolIDVideoTrim, "batch", []string{first, second}, trimOptions(1, 2)))
	if validationErr != nil {
		t.Fatalf("expected batch validation without outputDir to pass, got %v", validationErr)
	}
}

func TestTrimExecuteSingleUsesPlannedOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	stubVideoDuration(t, 0, false)
	runner := &stubCommandRunner{}
	tool := NewTrimToolWithDeps(stubRuntimeProbe{}, runner)

	item, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(1, 5)))
	if itemErr != nil {
		t.Fatalf("expected trim to succeed, got %v", itemErr)
	}

	want := filepath.Join(dir, "clip_trimmed.mp4")
	if item.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, item.OutputPath)
	}
	if len(runner.calls) == 0 {
		t.Fatal("expected the trim command to run")
	}
	assertAtomicallyPublished(t, runner.calls[0], want)
}

func TestTrimPlanRejectsStartBeyondKnownDuration(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	tool := newTrimPlanTool(t)
	stubVideoDuration(t, 10, true)

	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(30, 40)))
	if planErr == nil {
		t.Fatal("expected start beyond duration to fail")
	}
	if planErr.Code != engine.ErrorCodeVideoTrimInvalidTimeRange {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoTrimInvalidTimeRange, planErr.Code)
	}
	if !strings.Contains(planErr.Message, "duration") {
		t.Fatalf("expected a duration message, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestTrimPlanAllowsEndBeyondKnownDuration(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	tool := newTrimPlanTool(t)
	stubVideoDuration(t, 10, true)

	// ffmpeg stops at EOF, so a fixed endTime past the duration stays valid.
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(1, 30)))
	if planErr != nil {
		t.Fatalf("expected end past duration to pass, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected success plan")
	}
}
