package video

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/video/engine"
)

func newConvertPlanTool() *ConvertTool {
	return NewConvertToolWithDeps(stubRuntimeProbe{}, nil)
}

func TestConvertPlanSingleAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
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
	if want := filepath.Join(dir, "clip_converted.mp4"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.InputPath != input {
		t.Fatalf("expected input %q, got %q", input, output.InputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Exists || output.Adjusted {
		t.Fatalf("expected a fresh natural output, got %+v", output)
	}
	if len(plan.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", plan.Warnings)
	}
	if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}
}

func TestConvertPlanBatchWithoutOutputDirPlansPerInputOutputs(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.mp4")
	second := filepath.Join(root, "two", "second.mov")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "batch", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(root, "one", "first_converted.mp4"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(root, "two", "second_converted.mp4"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
}

func TestConvertPlanUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	req := videoToolRequest(ToolIDVideoConvert, "batch", []string{first, second}, map[string]any{"targetFormat": "webm"})
	req.OutputDir = outDir
	plan, planErr := newConvertPlanTool().Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(outDir, "first_converted.webm"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(outDir, "second_converted.webm"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
}

func TestConvertPlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)
	writeVideoFixture(t, filepath.Join(dir, "clip_converted.mp4"))

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "clip_converted-2.mp4"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 || len(plan.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestConvertPlanDefaultsMissingOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["targetFormat"]; got != "mp4" {
		t.Fatalf("expected default targetFormat mp4, got %v", got)
	}
	if got := plan.EffectiveOptions["qualityPreset"]; got != "medium" {
		t.Fatalf("expected default qualityPreset medium, got %v", got)
	}

	customDir := filepath.Join(dir, "custom")
	writeVideoFixture(t, filepath.Join(customDir, "clip.mkv"))
	customReq := videoToolRequest(ToolIDVideoConvert, "single", []string{filepath.Join(customDir, "clip.mkv")}, map[string]any{"targetFormat": "webm", "qualityPreset": "low"})
	customPlan, customErr := newConvertPlanTool().Plan(context.Background(), customReq)
	if customErr != nil {
		t.Fatalf("expected custom plan to succeed, got %v", customErr)
	}
	if got := customPlan.EffectiveOptions["targetFormat"]; got != "webm" {
		t.Fatalf("expected targetFormat webm, got %v", got)
	}
	if got := customPlan.EffectiveOptions["qualityPreset"]; got != "low" {
		t.Fatalf("expected qualityPreset low, got %v", got)
	}
	if want := filepath.Join(customDir, "clip_converted.webm"); customPlan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, customPlan.Outputs[0].OutputPath)
	}
}

func TestConvertPlanRejectsInvalidOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	cases := []struct {
		name    string
		options map[string]any
	}{
		{name: "invalid format", options: map[string]any{"targetFormat": "avi"}},
		{name: "invalid preset", options: map[string]any{"qualityPreset": "ultra"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, testCase.options))
			if planErr == nil {
				t.Fatal("expected a validation error")
			}
			if planErr.Code != engine.ErrorCodeVideoValidation {
				t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoValidation, planErr.Code)
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
		})
	}
}

func TestConvertPlanExplicitOutputConflict(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	outputPath := filepath.Join(dir, "custom.mp4")
	writeVideoFixture(t, input)
	writeVideoFixture(t, outputPath)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, map[string]any{"outputPath": outputPath}))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.Code != engine.ErrorCodeVideoOutputExists {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoOutputExists, planErr.Code)
	}
	if !strings.Contains(planErr.Message, "custom.mp4") {
		t.Fatalf("expected message to name the conflicting file, got %q", planErr.Message)
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

func TestConvertPlanExplicitOutputExtensionMismatch(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	req := videoToolRequest(ToolIDVideoConvert, "single", []string{input}, map[string]any{
		"outputPath":    filepath.Join(dir, "custom.webm"),
		"targetFormat":  "mp4",
		"qualityPreset": "medium",
	})
	_, planErr := newConvertPlanTool().Plan(context.Background(), req)
	if planErr == nil {
		t.Fatal("expected extension mismatch error")
	}
	if planErr.Code != engine.ErrorCodeVideoFormatMismatch {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoFormatMismatch, planErr.Code)
	}
}

func TestConvertValidateBatchWithoutOutputDir(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.mp4")
	second := filepath.Join(root, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	validationErr := newConvertPlanTool().Validate(context.Background(), videoToolRequest(ToolIDVideoConvert, "batch", []string{first, second}, nil))
	if validationErr != nil {
		t.Fatalf("expected batch validation without outputDir to pass, got %v", validationErr)
	}
}

func TestConvertPlanRuntimeUnavailable(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	tool := NewConvertToolWithDeps(stubRuntimeProbe{err: &engine.VideoError{Code: engine.ErrorCodeRuntimeFFmpegNotFound, Message: "ffmpeg missing"}}, nil)
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected runtime error")
	}
	if planErr.Code != engine.ErrorCodeRuntimeUnavailable {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeRuntimeUnavailable, planErr.Code)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
	if len(plan.Outputs) != 0 {
		t.Fatalf("expected no planned outputs, got %d", len(plan.Outputs))
	}
}

func TestConvertExecuteSingleUsesPlannedOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	runner := &stubCommandRunner{}
	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, runner)

	item, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
	if itemErr != nil {
		t.Fatalf("expected conversion to succeed, got %v", itemErr)
	}

	want := filepath.Join(dir, "clip_converted.mp4")
	if item.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, item.OutputPath)
	}
	if len(item.Outputs) != 1 || item.Outputs[0] != want {
		t.Fatalf("expected outputs to contain %q, got %v", want, item.Outputs)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	assertAtomicallyPublished(t, runner.calls[0], want)
}

func TestConvertPlanMissingInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "missing.mp4")

	_, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected missing input error")
	}
	if !strings.Contains(planErr.Message, "missing.mp4") {
		t.Fatalf("expected message to name the file, got %q", planErr.Message)
	}
}

func TestConvertPlanSingleInputCount(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	_, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{first, second}, nil))
	if planErr == nil {
		t.Fatal("expected input count error")
	}
	if planErr.Code != engine.ErrorCodeVideoValidation {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoValidation, planErr.Code)
	}
}

func TestConvertPlanIncludesVideoOutputControls(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	options := map[string]any{
		"resolution":  "720p",
		"bitrateKbps": 3200,
		"removeAudio": true,
	}
	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["resolution"]; got != "720p" {
		t.Fatalf("expected resolution 720p, got %v", got)
	}
	if got := plan.EffectiveOptions["bitrateKbps"]; got != 3200 {
		t.Fatalf("expected bitrateKbps 3200, got %v", got)
	}
	if got := plan.EffectiveOptions["removeAudio"]; got != true {
		t.Fatalf("expected removeAudio true, got %v", got)
	}
}

func TestConvertPlanDefaultsVideoOutputControls(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["resolution"]; got != "original" {
		t.Fatalf("expected default resolution original, got %v", got)
	}
	if got := plan.EffectiveOptions["bitrateKbps"]; got != 0 {
		t.Fatalf("expected default bitrateKbps 0, got %v", got)
	}
	if got := plan.EffectiveOptions["removeAudio"]; got != false {
		t.Fatalf("expected default removeAudio false, got %v", got)
	}
}

func TestConvertPlanRejectsInvalidVideoOutputControls(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	cases := []struct {
		name    string
		options map[string]any
	}{
		{name: "invalid resolution", options: map[string]any{"resolution": "4k"}},
		{name: "negative bitrate", options: map[string]any{"bitrateKbps": -1}},
		{name: "non numeric bitrate", options: map[string]any{"bitrateKbps": "fast"}},
		{name: "non boolean removeAudio", options: map[string]any{"removeAudio": "loud"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, planErr := newConvertPlanTool().Plan(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, testCase.options))
			if planErr == nil {
				t.Fatal("expected a validation error")
			}
			if planErr.Code != engine.ErrorCodeVideoValidation {
				t.Fatalf("expected %q, got %q", engine.ErrorCodeVideoValidation, planErr.Code)
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
		})
	}
}

func TestConvertExecuteSingleAppliesVideoOutputControls(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, input)

	runner := &stubCommandRunner{}
	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, runner)
	options := map[string]any{
		"resolution":  "720p",
		"bitrateKbps": 3200,
		"removeAudio": true,
	}

	if _, itemErr := tool.ExecuteSingle(context.Background(), videoToolRequest(ToolIDVideoConvert, "single", []string{input}, options)); itemErr != nil {
		t.Fatalf("expected conversion to succeed, got %v", itemErr)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	args := runner.calls[0]
	for _, want := range [][]string{
		{"-vf", "scale=-2:720"},
		{"-b:v", "3200k"},
		{"-an"},
		{"-c:v", "libx264"},
	} {
		if !hasArgSequence(args, want...) {
			t.Fatalf("expected %v in ffmpeg args, got %v", want, args)
		}
	}
	if hasArgSequence(args, "-crf") {
		t.Fatalf("explicit bitrate must omit -crf, got %v", args)
	}
	if hasArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
}

func TestConvertExecuteBatchAppliesVideoOutputControls(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeVideoFixture(t, first)
	writeVideoFixture(t, second)

	runner := &stubCommandRunner{}
	tool := NewConvertToolWithDeps(stubRuntimeProbe{}, runner)
	options := map[string]any{"resolution": "480p", "removeAudio": true}

	items, batchErr := tool.ExecuteBatch(context.Background(), videoToolRequest(ToolIDVideoConvert, "batch", []string{first, second}, options), nil)
	if batchErr != nil {
		t.Fatalf("expected batch conversion to succeed, got %v", batchErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected 2 ffmpeg calls, got %d", len(runner.calls))
	}

	for _, args := range runner.calls {
		if !hasArgSequence(args, "-vf", "scale=-2:480") {
			t.Fatalf("expected the 480p scale filter in %v", args)
		}
		if !hasArgSequence(args, "-an") {
			t.Fatalf("expected -an in %v", args)
		}
	}
}
