package audio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/audio/engine"
)

func TestExtractPlanAutoOutputAndVideoInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeAudioFixture(t, input)

	probe, encoders := planDeps()
	plan, planErr := NewExtractToolWithDeps(probe, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioExtract, "single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success || len(plan.Outputs) != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "clip_audio.mp3"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.OutputKind != "file" || output.Exists || output.Adjusted {
		t.Fatalf("unexpected planned output: %+v", output)
	}
	if got := plan.EffectiveOptions["format"]; got != "mp3" {
		t.Fatalf("expected default format mp3, got %v", got)
	}
	if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}
}

func TestExtractPlanUsesChosenFormatAndOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	first := filepath.Join(dir, "first.mkv")
	second := filepath.Join(dir, "second.mov")
	writeAudioFixture(t, first)
	writeAudioFixture(t, second)

	probe, encoders := planDeps()
	req := audioToolRequest(ToolIDAudioExtract, "batch", []string{first, second}, map[string]any{"format": "flac"})
	req.OutputDir = outDir
	plan, planErr := NewExtractToolWithDeps(probe, nil, encoders).Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(outDir, "first_audio.flac"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(outDir, "second_audio.flac"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
}

func TestConvertPlanRejectsUnsupportedInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeAudioFixture(t, input)

	probe, encoders := planDeps()
	plan, planErr := NewConvertToolWithDeps(probe, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected convert to reject a video input")
	}
	if planErr.Code != engine.ErrorCodeInputUnsupported {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeInputUnsupported, planErr.Code)
	}
	if plan.Success {
		t.Fatal("expected plan failure")
	}
}

func TestPlanRejectsUnsupportedTargetFormat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	writeAudioFixture(t, input)

	probe, encoders := planDeps()
	_, planErr := NewConvertToolWithDeps(probe, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, map[string]any{"format": "ogg"}))
	if planErr == nil {
		t.Fatal("expected an unsupported format error")
	}
	if planErr.Code != engine.ErrorCodeFormatUnsupported {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeFormatUnsupported, planErr.Code)
	}
}

func TestPlanRejectsMissingEncoder(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	writeAudioFixture(t, input)

	// The build only provides AAC, so mp3 planning must fail before encoding.
	encoders := encoderProbe("aac")
	_, planErr := NewConvertToolWithDeps(stubRuntimeProbe{}, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, map[string]any{"format": "mp3"}))
	if planErr == nil {
		t.Fatal("expected a missing encoder error")
	}
	if planErr.Code != engine.ErrorCodeEncoderUnavailable {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeEncoderUnavailable, planErr.Code)
	}
	if !strings.Contains(strings.ToLower(planErr.Message), "libmp3lame") {
		t.Fatalf("expected actionable guidance naming the encoder, got %q", planErr.Message)
	}
}

func TestPlanRejectsMissingRuntime(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	writeAudioFixture(t, input)

	probe := stubRuntimeProbe{err: &engine.AudioError{
		Code:    engine.ErrorCodeRuntimeFFmpegNotFound,
		Message: "ffmpeg binary was not found in PATH",
	}}
	_, planErr := NewExtractToolWithDeps(probe, nil, encoderProbe()).Plan(context.Background(),
		audioToolRequest(ToolIDAudioExtract, "single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected a runtime error")
	}
	if planErr.Code != engine.ErrorCodeRuntimeUnavailable {
		t.Fatalf("expected %q, got %q", engine.ErrorCodeRuntimeUnavailable, planErr.Code)
	}
}

func TestPlanCollisionSafeNaming(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	writeAudioFixture(t, input)
	writeAudioFixture(t, filepath.Join(dir, "clip_converted.mp3"))

	probe, encoders := planDeps()
	plan, planErr := NewConvertToolWithDeps(probe, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "clip_converted-2.mp3"); output.OutputPath != want {
		t.Fatalf("expected collision-safe output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected the adjusted flag")
	}
	if len(output.Warnings) == 0 || len(plan.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestPlanBatchPlansOneOutputPerInput(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.wav")
	second := filepath.Join(root, "two", "second.flac")
	writeAudioFixture(t, first)
	writeAudioFixture(t, second)

	probe, encoders := planDeps()
	plan, planErr := NewConvertToolWithDeps(probe, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "batch", []string{first, second}, map[string]any{"format": "m4a"}))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(root, "one", "first_converted.m4a"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(root, "two", "second_converted.m4a"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[1].OutputPath)
	}
}

func TestPlanWarnsWhenBitrateIsIgnoredForLossless(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	writeAudioFixture(t, input)

	probe, encoders := planDeps()
	plan, planErr := NewConvertToolWithDeps(probe, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, map[string]any{"format": "flac", "bitrateKbps": 320}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["bitrateKbps"]; got != 0 {
		t.Fatalf("expected bitrate to be dropped, got %v", got)
	}
	found := false
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "ignored") && strings.Contains(warning, "lossless") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a lossless bitrate warning, got %v", plan.Warnings)
	}
}

func TestNormalizePlanPreservesContainerAndTargets(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	writeAudioFixture(t, input)

	probe, encoders := planDeps()
	plan, planErr := NewNormalizeToolWithDeps(probe, nil, encoders).Plan(context.Background(),
		audioToolRequest(ToolIDAudioNormalize, "single", []string{input}, map[string]any{"targetLufs": "broadcast"}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if want := filepath.Join(dir, "clip_normalized.wav"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if got := plan.EffectiveOptions["targetLufs"]; got != "broadcast" {
		t.Fatalf("expected broadcast target, got %v", got)
	}
	if got := plan.EffectiveOptions["targetLufsValue"]; got != float64(-23) {
		t.Fatalf("expected -23 LUFS, got %v", got)
	}
	if got := plan.EffectiveOptions["truePeak"]; got != engine.DefaultTruePeak {
		t.Fatalf("expected default true peak, got %v", got)
	}
	if !strings.Contains(plan.Message, "LUFS") {
		t.Fatalf("expected the plan message to state the target, got %q", plan.Message)
	}
}

func TestNormalizePlanAcceptsNumericTargetAndValidatesOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp3")
	writeAudioFixture(t, input)
	probe, encoders := planDeps()
	tool := NewNormalizeToolWithDeps(probe, nil, encoders)

	plan, planErr := tool.Plan(context.Background(),
		audioToolRequest(ToolIDAudioNormalize, "single", []string{input}, map[string]any{"targetLufs": -16}))
	if planErr != nil {
		t.Fatalf("expected numeric target to plan, got %v", planErr)
	}
	if got := plan.EffectiveOptions["targetLufs"]; got != "podcast" {
		t.Fatalf("expected -16 to resolve to podcast, got %v", got)
	}
	if want := filepath.Join(dir, "clip_normalized.mp3"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}

	cases := []struct {
		name    string
		options map[string]any
		want    string
	}{
		{name: "invalid target", options: map[string]any{"targetLufs": "loud"}, want: engine.ErrorCodeTargetLufsInvalid},
		{name: "invalid true peak", options: map[string]any{"truePeak": 2}, want: engine.ErrorCodeTruePeakInvalid},
		{name: "format is not an option", options: map[string]any{"format": "mp3"}, want: engine.ErrorCodeValidation},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := tool.Plan(context.Background(),
				audioToolRequest(ToolIDAudioNormalize, "single", []string{input}, testCase.options))
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if err.Code != testCase.want {
				t.Fatalf("expected %q, got %q (%s)", testCase.want, err.Code, err.Message)
			}
		})
	}
}

func TestExecutePlanHonorsMutatedOutputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.wav")
	writeAudioFixture(t, input)
	mutated := filepath.Join(dir, "custom.mp3")

	probe, encoders := planDeps()
	tool := NewConvertToolWithDeps(probe, &stubRunner{}, encoders)
	plan, planErr := tool.Plan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	plan.Outputs[0].OutputPath = mutated

	items, execErr := tool.ExecutePlan(context.Background(),
		audioToolRequest(ToolIDAudioConvert, "single", []string{input}, nil), plan, nil)
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

// stubRunner writes a placeholder output without running ffmpeg.
type stubRunner struct {
	calls [][]string
	run   func(args []string) error
}

func (r *stubRunner) Run(_ context.Context, name string, args []string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.run != nil {
		return r.run(args)
	}

	return os.WriteFile(args[len(args)-1], []byte("fixture"), 0o644)
}
