package video

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/video/engine"
)

func newMergePlanTool() *MergeTool {
	return NewMergeToolWithDeps(stubRuntimeProbe{}, nil)
}

func TestMergePlanAutoOutputNextToFirstInput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, nil))
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
	if want := filepath.Join(dir, "first_merged.mp4"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Adjusted || output.Exists {
		t.Fatalf("expected a fresh natural output, got %+v", output)
	}
}

func TestMergePlanDefaultsOptions(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["targetFormat"]; got != "mp4" {
		t.Fatalf("expected default targetFormat mp4, got %v", got)
	}
	if got := plan.EffectiveOptions["qualityPreset"]; got != "medium" {
		t.Fatalf("expected default qualityPreset medium, got %v", got)
	}
	if got := plan.EffectiveOptions["mergeMode"]; got != engine.MergeModeAuto {
		t.Fatalf("expected default mergeMode auto, got %v", got)
	}
}

func TestMergePlanUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	req := videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, map[string]any{"targetFormat": "webm"})
	req.OutputDir = outDir
	plan, planErr := newMergePlanTool().Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(outDir, "first_merged.webm"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestMergePlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)
	writeVideoFixture(t, filepath.Join(dir, "first_merged.mp4"))

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "first_merged-2.mp4"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestMergePlanExplicitOutputConflict(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	outputPath := filepath.Join(dir, "merged.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)
	writeVideoFixture(t, outputPath)

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, map[string]any{"outputPath": outputPath}))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.Code != engine.ErrorCodeVideoMergeOutputExists {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoMergeOutputExists, planErr.Code)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
	if len(plan.Outputs) != 1 || !plan.Outputs[0].Exists {
		t.Fatal("expected the conflicting output to be reported as existing")
	}

	content, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "fixture" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestMergeExecuteSingleUsesPlannedOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	runner := &stubCommandRunner{}
	tool := NewMergeToolWithDeps(stubRuntimeProbe{}, runner)

	item, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, nil))
	if itemErr != nil {
		t.Fatalf("expected merge to succeed, got %v", itemErr)
	}

	want := filepath.Join(dir, "first_merged.mp4")
	if item.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, item.OutputPath)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	if got := runner.calls[0][len(runner.calls[0])-1]; got != want {
		t.Fatalf("expected ffmpeg output argument %q, got %q", want, got)
	}
}

func TestMergePlanRejectsFewerThanTwoInputs(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	writeVideoFixture(t, first)

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first}, nil))
	if planErr == nil {
		t.Fatal("expected insufficient inputs error")
	}
	if planErr.Code != engine.ErrorCodeVideoMergeInsufficientInputs {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoMergeInsufficientInputs, planErr.Code)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestMergePlanMissingInput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	missing := filepath.Join(dir, "missing.mp4")
	writeVideoFixture(t, first)

	_, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, missing}, nil))
	if planErr == nil {
		t.Fatal("expected missing input error")
	}
	if !strings.Contains(planErr.Message, "missing.mp4") {
		t.Fatalf("expected message to name the file, got %q", planErr.Message)
	}
}

func TestMergeValidateWithoutOutputPath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	validationErr := newMergePlanTool().Validate(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, nil))
	if validationErr != nil {
		t.Fatalf("expected merge validation without outputPath to pass, got %v", validationErr)
	}
}
