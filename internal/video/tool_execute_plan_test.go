package video

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/tools"
)

// Every video tool exposes the shared plan-once contract.
var (
	_ tools.PlanExecutor = (*ConvertTool)(nil)
	_ tools.PlanExecutor = (*TrimTool)(nil)
	_ tools.PlanExecutor = (*MergeTool)(nil)
	_ tools.PlanExecutor = (*TranscribeTool)(nil)
)

func TestConvertExecutePlanHonorsMutatedOutputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	mutated := filepath.Join(dir, "custom-target.mp4")

	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, &stubCommandRunner{})
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	plan.Outputs[0].OutputPath = mutated

	items, execErr := tool.ExecutePlan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil), plan, nil)
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].OutputPath != mutated {
		t.Fatalf("expected the mutated path %q, got %q", mutated, items[0].OutputPath)
	}
	if _, statErr := os.Stat(mutated); statErr != nil {
		t.Fatalf("expected the mutated path to be published: %v", statErr)
	}
}

func TestConvertExecutePlanProcessesOnlyPlannedOutputs(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "one.mp4"),
		filepath.Join(dir, "two.mp4"),
		filepath.Join(dir, "three.mp4"),
	}
	for _, input := range inputs {
		writeVideoFixture(t, input)
	}

	runner := &stubCommandRunner{}
	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, runner)
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "batch", inputs, nil))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	subset := plan
	subset.Outputs = []models.PlannedOutput{plan.Outputs[2], plan.Outputs[0]}
	items, execErr := tool.ExecutePlan(context.Background(), videoToolRequest(ToolIDVideoConvert, "batch", inputs, nil), subset, nil)
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected 2 ffmpeg calls, got %d", len(runner.calls))
	}
	if items[0].InputPath != inputs[2] || items[1].InputPath != inputs[0] {
		t.Fatalf("expected plan order %q then %q, got %+v", inputs[2], inputs[0], items)
	}

	for _, outputPath := range []string{plan.Outputs[0].OutputPath, plan.Outputs[2].OutputPath} {
		if _, statErr := os.Stat(outputPath); statErr != nil {
			t.Fatalf("expected the planned output %q: %v", outputPath, statErr)
		}
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("plan subset must not create %q, stat err: %v", plan.Outputs[1].OutputPath, statErr)
	}
}

func TestConvertExecutePlanIsolatesItemFailures(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	runner := &stubCommandRunner{run: func(_ string, args []string) error {
		if hasArgSequence(args, second) {
			return errors.New("ffmpeg exploded")
		}
		return writeStubCommandOutput(args)
	}}

	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, runner)
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "batch", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	items, execErr := tool.ExecutePlan(context.Background(), videoToolRequest(ToolIDVideoConvert, "batch", []string{first, second}, nil), plan, nil)
	if execErr == nil {
		t.Fatal("expected the batch error to be reported")
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].Success {
		t.Fatalf("expected the first item to succeed, got %+v", items[0])
	}
	if items[1].Success || items[1].Error == nil {
		t.Fatalf("expected the second item to fail, got %+v", items[1])
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); statErr != nil {
		t.Fatalf("expected the first output: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed item must not publish an output, stat err: %v", statErr)
	}
}

func TestTrimExecutePlanHonorsMutatedOutputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	mutated := filepath.Join(dir, "custom-trim.mp4")
	stubVideoDuration(t, 0, false)

	tool := NewTrimToolWithDeps(stubRuntimeProbe{}, &stubCommandRunner{})
	req := videoToolRequest(ToolIDVideoTrim, "single", []string{input}, trimOptions(1, 5))
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	plan.Outputs[0].OutputPath = mutated

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 1 || items[0].OutputPath != mutated {
		t.Fatalf("expected the mutated path %q, got %+v", mutated, items)
	}
	if _, statErr := os.Stat(mutated); statErr != nil {
		t.Fatalf("expected the mutated path to be published: %v", statErr)
	}
}

func TestTrimExecutePlanProcessesOnlyPlannedOutputsInOrder(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "one.mp4"),
		filepath.Join(dir, "two.mp4"),
		filepath.Join(dir, "three.mp4"),
	}
	for _, input := range inputs {
		writeVideoFixture(t, input)
	}
	stubVideoDuration(t, 0, false)

	runner := &stubCommandRunner{}
	tool := NewTrimToolWithDeps(stubRuntimeProbe{}, runner)
	req := videoToolRequest(ToolIDVideoTrim, "batch", inputs, trimOptions(0, 2))
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	subset := plan
	subset.Outputs = []models.PlannedOutput{plan.Outputs[1]}
	items, execErr := tool.ExecutePlan(context.Background(), req, subset, nil)
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 1 || items[0].InputPath != inputs[1] {
		t.Fatalf("expected only the second planned output, got %+v", items)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); statErr != nil {
		t.Fatalf("expected the planned output: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("plan subset must not create %q, stat err: %v", plan.Outputs[0].OutputPath, statErr)
	}
}

func TestTrimExecutePlanIsolatesItemFailures(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)
	stubVideoDuration(t, 0, false)

	runner := &stubCommandRunner{run: func(_ string, args []string) error {
		if hasArgSequence(args, second) {
			return errors.New("ffmpeg exploded")
		}
		return writeStubCommandOutput(args)
	}}

	tool := NewTrimToolWithDeps(stubRuntimeProbe{}, runner)
	req := videoToolRequest(ToolIDVideoTrim, "batch", []string{first, second}, trimOptions(0, 2))
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr == nil {
		t.Fatal("expected the batch error to be reported")
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].Success || items[1].Success || items[1].Error == nil {
		t.Fatalf("expected one success and one isolated failure, got %+v", items)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); statErr != nil {
		t.Fatalf("expected the first output: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed item must not publish an output, stat err: %v", statErr)
	}
}

func TestMergeExecutePlanHonorsMutatedOutputPath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)
	mutated := filepath.Join(dir, "custom-merge.mp4")

	tool := NewMergeToolWithDeps(stubRuntimeProbe{}, &stubCommandRunner{})
	req := videoToolRequest(ToolIDVideoMerge, "single", []string{first, second}, nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	plan.Outputs[0].OutputPath = mutated

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 1 || items[0].OutputPath != mutated {
		t.Fatalf("expected the mutated path %q, got %+v", mutated, items)
	}
	if _, statErr := os.Stat(mutated); statErr != nil {
		t.Fatalf("expected the mutated path to be published: %v", statErr)
	}
}

func TestTranscribeExecutePlanHonorsMutatedOutputPath(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)
	mutated := filepath.Join(dir, "custom-subtitles.srt")

	runner := newFakeToolTranscribeRunner()
	tool := NewTranscribeToolWithDeps(runner, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	req := videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, map[string]any{"formats": []string{"srt"}})
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}
	plan.Outputs[0].OutputPath = mutated

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 1 || items[0].OutputPath != mutated {
		t.Fatalf("expected the mutated path %q, got %+v", mutated, items)
	}
	if _, statErr := os.Stat(mutated); statErr != nil {
		t.Fatalf("expected the mutated path to be published: %v", statErr)
	}
}

func TestTranscribeExecutePlanProcessesOnlyPlannedOutputs(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	firstInput := filepath.Join(firstDir, "first.mp4")
	secondInput := filepath.Join(secondDir, "second.mp4")
	writeVideoFixture(t, firstInput)
	writeVideoFixture(t, secondInput)
	binaryPath, modelPath := writeTranscribeDeps(t, t.TempDir())
	outputDir := t.TempDir()

	runner := newFakeToolTranscribeRunner()
	tool := NewTranscribeToolWithDeps(runner, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	req := videoToolRequest(ToolIDVideoTranscribe, "batch", []string{firstInput, secondInput}, nil)
	req.OutputDir = outputDir
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	if len(plan.Outputs) != 6 {
		t.Fatalf("expected 6 planned outputs, got %d", len(plan.Outputs))
	}

	subset := plan
	subset.Outputs = append([]models.PlannedOutput(nil), plan.Outputs[:3]...)
	items, execErr := tool.ExecutePlan(context.Background(), req, subset, nil)
	if execErr != nil {
		t.Fatalf("unexpected execution error: %v", execErr)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items (one per planned output), got %d", len(items))
	}
	for index, item := range items {
		if item.InputPath != firstInput || item.OutputPath != subset.Outputs[index].OutputPath {
			t.Fatalf("unexpected item %d: %+v", index, item)
		}
		if _, statErr := os.Stat(item.OutputPath); statErr != nil {
			t.Fatalf("expected the planned output %q: %v", item.OutputPath, statErr)
		}
	}
	for _, planned := range plan.Outputs[3:] {
		if _, statErr := os.Stat(planned.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("plan subset must not create %q, stat err: %v", planned.OutputPath, statErr)
		}
	}

	whisperCalls := 0
	for _, call := range runner.snapshotCalls() {
		if strings.HasPrefix(call, binaryPath+" ") {
			whisperCalls++
		}
	}
	if whisperCalls != 1 {
		t.Fatalf("expected one whisper run for the planned input, got %d", whisperCalls)
	}
}

func TestTranscribeExecutePlanIsolatesItemFailures(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	firstInput := filepath.Join(firstDir, "first.mp4")
	secondInput := filepath.Join(secondDir, "second.mp4")
	writeVideoFixture(t, firstInput)
	writeVideoFixture(t, secondInput)
	binaryPath, modelPath := writeTranscribeDeps(t, t.TempDir())
	outputDir := t.TempDir()

	runner := newFakeToolTranscribeRunner()
	runner.failOnWhisperCall = 2
	tool := NewTranscribeToolWithDeps(runner, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	req := videoToolRequest(ToolIDVideoTranscribe, "batch", []string{firstInput, secondInput}, nil)
	req.OutputDir = outputDir
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr == nil {
		t.Fatal("expected the batch error to be reported")
	}
	if len(items) != len(plan.Outputs) {
		t.Fatalf("expected one item per planned output (%d), got %d", len(plan.Outputs), len(items))
	}
	for index, item := range items {
		if item.InputPath != plan.Outputs[index].InputPath || item.OutputPath != plan.Outputs[index].OutputPath {
			t.Fatalf("plan order drifted at %d: item=%+v planned=%+v", index, item, plan.Outputs[index])
		}
		if index < 3 && !item.Success {
			t.Fatalf("expected the first input to succeed at %d, got %+v", index, item)
		}
		if index >= 3 && (item.Success || item.Error == nil) {
			t.Fatalf("expected the second input to fail at %d, got %+v", index, item)
		}
	}
	for _, planned := range plan.Outputs[3:] {
		if _, statErr := os.Stat(planned.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("failed input must not publish %q, stat err: %v", planned.OutputPath, statErr)
		}
	}
}
