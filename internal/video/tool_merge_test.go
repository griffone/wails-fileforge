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
	assertAtomicallyPublished(t, runner.calls[0], want)
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

func TestMergePlanIncludesRemoveAudio(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	options := map[string]any{"removeAudio": true}
	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["removeAudio"]; got != true {
		t.Fatalf("expected removeAudio true, got %v", got)
	}
}

func TestMergePlanDefaultsRemoveAudio(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["removeAudio"]; got != false {
		t.Fatalf("expected default removeAudio false, got %v", got)
	}
}

func TestMergePlanRejectsInvalidRemoveAudio(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	options := map[string]any{"removeAudio": "loud"}
	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, options))
	if planErr == nil {
		t.Fatal("expected a validation error")
	}
	if planErr.Code != engine.ErrorCodeVideoMergeValidation {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoMergeValidation, planErr.Code)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestMergeExecuteSingleAppliesRemoveAudio(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	runner := &stubCommandRunner{}
	tool := NewMergeToolWithDeps(stubRuntimeProbe{}, runner)
	options := map[string]any{"removeAudio": true, "mergeMode": engine.MergeModeReencode}

	if _, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, options)); itemErr != nil {
		t.Fatalf("expected merge to succeed, got %v", itemErr)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	args := runner.calls[0]
	if !hasArgSequence(args, "-an") {
		t.Fatalf("expected -an in ffmpeg args, got %v", args)
	}
	if hasArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
}

func TestMergeExecuteCopyAppliesRemoveAudio(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	runner := &stubCommandRunner{}
	tool := NewMergeToolWithDeps(stubRuntimeProbe{}, runner)
	options := map[string]any{"removeAudio": true, "mergeMode": engine.MergeModeCopy}

	if _, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, options)); itemErr != nil {
		t.Fatalf("expected merge to succeed, got %v", itemErr)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	args := runner.calls[0]
	if !hasArgSequence(args, "-c", "copy") {
		t.Fatalf("expected stream copy, got %v", args)
	}
	if !hasArgSequence(args, "-an") {
		t.Fatalf("expected -an in ffmpeg args, got %v", args)
	}
}

// stubMergeInputDuration simulates probed clip durations for crossfade
// execution so tests never depend on ffprobe or real video files.
func stubMergeInputDuration(t *testing.T, duration float64) {
	t.Helper()

	original := engine.MergeInputDurationProbe
	t.Cleanup(func() {
		engine.MergeInputDurationProbe = original
	})

	engine.MergeInputDurationProbe = func(context.Context, string) (float64, bool) {
		return duration, true
	}
}

func TestMergePlanDefaultsTransitionOptions(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["transition"]; got != engine.MergeTransitionNone {
		t.Fatalf("expected default transition none, got %v", got)
	}
	if got := plan.EffectiveOptions["transitionDuration"]; got != engine.MergeTransitionDurationDefault {
		t.Fatalf("expected default transitionDuration %v, got %v", engine.MergeTransitionDurationDefault, got)
	}
}

func TestMergePlanIncludesTransitionOptions(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	options := map[string]any{"transition": "crossfade", "transitionDuration": 1.5}
	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["transition"]; got != engine.MergeTransitionCrossfade {
		t.Fatalf("expected transition crossfade, got %v", got)
	}
	if got := plan.EffectiveOptions["transitionDuration"]; got != 1.5 {
		t.Fatalf("expected transitionDuration 1.5, got %v", got)
	}
}

func TestMergePlanRejectsInvalidTransitionOptions(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	tests := []struct {
		name    string
		options map[string]any
	}{
		{name: "unknown transition", options: map[string]any{"transition": "dissolve"}},
		{name: "crossfade duration too short", options: map[string]any{"transition": "crossfade", "transitionDuration": 0.05}},
		{name: "crossfade duration too long", options: map[string]any{"transition": "crossfade", "transitionDuration": 4}},
		{name: "non numeric duration", options: map[string]any{"transition": "crossfade", "transitionDuration": "fast"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, test.options))
			if planErr == nil {
				t.Fatal("expected a validation error")
			}
			if planErr.Code != engine.ErrorCodeVideoMergeValidation {
				t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoMergeValidation, planErr.Code)
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
		})
	}
}

func TestMergePlanAllowsNoneTransitionWithAnyDuration(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	options := map[string]any{"transitionDuration": 5}
	plan, planErr := newMergePlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["transition"]; got != engine.MergeTransitionNone {
		t.Fatalf("expected transition none, got %v", got)
	}
}

func TestMergeExecuteSingleUsesCrossfade(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	runner := &stubCommandRunner{}
	tool := NewMergeToolWithDeps(stubRuntimeProbe{}, runner)
	stubMergeInputDuration(t, 10)
	options := map[string]any{"transition": "crossfade", "transitionDuration": 1, "mergeMode": engine.MergeModeCopy}

	item, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoMergeV1, "single", []string{first, second}, options))
	if itemErr != nil {
		t.Fatalf("expected merge to succeed, got %v", itemErr)
	}
	if item.OutputPath != filepath.Join(dir, "first_merged.mp4") {
		t.Fatalf("unexpected output path %q", item.OutputPath)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	args := runner.calls[0]
	filterComplex := ""
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "-filter_complex" {
			filterComplex = args[index+1]
		}
	}
	if filterComplex == "" || !strings.Contains(filterComplex, "xfade=transition=fade") {
		t.Fatalf("expected a crossfade filter chain, got %v", args)
	}
	if !hasArgSequence(args, "-map", "[vout]") {
		t.Fatalf("expected the final video label to be mapped, got %v", args)
	}
	if hasArgSequence(args, "-c", "copy") {
		t.Fatalf("crossfade must not stream copy, got %v", args)
	}
}
