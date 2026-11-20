package video

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

func writeTranscribeDeps(t *testing.T, dir string) (string, string) {
	t.Helper()

	binaryPath := filepath.Join(dir, "whisper-cli")
	modelPath := filepath.Join(dir, "ggml-base.bin")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write whisper fixture: %v", err)
	}
	if err := os.WriteFile(modelPath, []byte("model"), 0o644); err != nil {
		t.Fatalf("write model fixture: %v", err)
	}

	return binaryPath, modelPath
}

// newStubTranscribeCapabilities echoes provided paths and reports both
// dependencies present. Tests that need to simulate a missing runtime pass the
// present flags explicitly.
func newStubTranscribeCapabilities(whisperPath, modelPath string, whisperPresent, modelPresent bool) func(string, string) engine.TranscriptionCapabilities {
	return func(providedWhisper, providedModel string) engine.TranscriptionCapabilities {
		resolvedWhisper := providedWhisper
		if resolvedWhisper == "" {
			resolvedWhisper = whisperPath
		}
		resolvedModel := providedModel
		if resolvedModel == "" {
			resolvedModel = modelPath
		}

		capabilities := engine.TranscriptionCapabilities{
			WhisperPath:     resolvedWhisper,
			WhisperPresent:  whisperPresent,
			ModelPath:       resolvedModel,
			ModelPresent:    modelPresent,
			Languages:       engine.SupportedTranscriptionLanguages(),
			DefaultLanguage: engine.TranscribeLanguageAuto,
		}
		if modelPresent {
			capabilities.ModelName = filepath.Base(resolvedModel)
			capabilities.ModelSizeBytes = 100
		}

		return capabilities
	}
}

func plannedTranscribeOutput(t *testing.T, plan models.PlanJobResponse, inputPath, format string) models.PlannedOutput {
	t.Helper()

	for _, planned := range plan.Outputs {
		if planned.InputPath != inputPath {
			continue
		}
		if strings.TrimPrefix(strings.ToLower(filepath.Ext(planned.OutputPath)), ".") == format {
			return planned
		}
	}

	t.Fatalf("no planned %s output for %s in %+v", format, inputPath, plan.Outputs)

	return models.PlannedOutput{}
}

func TestTranscribeToolManifest(t *testing.T) {
	tool := NewTranscribeTool()
	manifest := tool.Manifest()

	if tool.ID() != ToolIDVideoTranscribe || tool.Capability() != ToolIDVideoTranscribe {
		t.Fatalf("unexpected tool identity: %s / %s", tool.ID(), tool.Capability())
	}
	if manifest.Name != "Video Transcribe" {
		t.Fatalf("unexpected name: %q", manifest.Name)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatal("expected single and batch support")
	}
	if manifest.Domain != "video" {
		t.Fatalf("unexpected domain: %q", manifest.Domain)
	}
	if strings.Join(manifest.OutputExtensions, ",") != "srt,vtt,txt" {
		t.Fatalf("unexpected output extensions: %v", manifest.OutputExtensions)
	}
	if strings.Join(manifest.RuntimeDeps, ",") != "whisper-cli,ffmpeg" {
		t.Fatalf("unexpected runtime dependencies: %v", manifest.RuntimeDeps)
	}
	if strings.Join(manifest.Tags, ",") != "video,audio,transcribe,whisper,subtitles" {
		t.Fatalf("unexpected tags: %v", manifest.Tags)
	}

	for _, extension := range []string{"mp4", "mov", "mkv", "webm", "avi", "mp3", "m4a", "wav", "ogg", "flac"} {
		if !hasString(manifest.InputExtensions, extension) {
			t.Fatalf("expected input extension %q in %v", extension, manifest.InputExtensions)
		}
	}
}

func TestTranscribeToolRuntimeState(t *testing.T) {
	dir := t.TempDir()
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	available := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	if state := available.RuntimeState(context.Background()); state.Status != "enabled" || !state.Healthy {
		t.Fatalf("expected an enabled runtime, got %+v", state)
	}

	missing := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, false, true))
	state := missing.RuntimeState(context.Background())
	if state.Status != "degraded" || state.Healthy {
		t.Fatalf("expected a degraded runtime, got %+v", state)
	}
	if !strings.Contains(state.Reason, "not found") || !strings.Contains(state.Reason, "Settings") {
		t.Fatalf("expected actionable guidance, got %q", state.Reason)
	}
}

func TestTranscribePlanDefaultsAllFormats(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, nil))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected a successful plan")
	}
	if len(plan.Outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(plan.Outputs))
	}

	for _, format := range []string{"srt", "vtt", "txt"} {
		planned := plannedTranscribeOutput(t, plan, inputPath, format)
		want := filepath.Join(dir, "clip."+format)
		if planned.OutputPath != want {
			t.Fatalf("expected %q, got %q", want, planned.OutputPath)
		}
		if planned.OutputKind != "file" || planned.OutputCount != 1 {
			t.Fatalf("unexpected output metadata: %+v", planned)
		}
		if planned.Adjusted || planned.Exists {
			t.Fatalf("expected a natural, absent output: %+v", planned)
		}
	}

	if got := plan.EffectiveOptions["language"]; got != "auto" {
		t.Fatalf("expected auto language, got %v", got)
	}
	if got := plan.EffectiveOptions["whisperPath"]; got != binaryPath {
		t.Fatalf("expected %q, got %v", binaryPath, got)
	}
	if got := plan.EffectiveOptions["modelPath"]; got != modelPath {
		t.Fatalf("expected %q, got %v", modelPath, got)
	}
	formats, ok := plan.EffectiveOptions["formats"].([]string)
	if !ok || strings.Join(formats, ",") != "srt,vtt,txt" {
		t.Fatalf("unexpected effective formats: %v", plan.EffectiveOptions["formats"])
	}
	if !strings.Contains(plan.Message, "1 file") {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}
}

func TestTranscribePlanExplicitOptions(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.wav")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)
	customBinary := filepath.Join(dir, "custom-whisper")
	customModel := filepath.Join(dir, "ggml-large.bin")
	writeVideoFixture(t, customBinary)
	writeVideoFixture(t, customModel)

	tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, map[string]any{
		"language":    "ES",
		"formats":     []string{"vtt"},
		"whisperPath": customBinary,
		"modelPath":   customModel,
	}))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	if len(plan.Outputs) != 1 {
		t.Fatalf("expected only the VTT output, got %+v", plan.Outputs)
	}
	if got := filepath.Ext(plan.Outputs[0].OutputPath); got != ".vtt" {
		t.Fatalf("expected a .vtt output, got %q", got)
	}
	if got := plan.EffectiveOptions["language"]; got != "es" {
		t.Fatalf("expected a lowercased language, got %v", got)
	}
	if got := plan.EffectiveOptions["whisperPath"]; got != customBinary {
		t.Fatalf("expected the explicit whisper path %q, got %v", customBinary, got)
	}
	if got := plan.EffectiveOptions["modelPath"]; got != customModel {
		t.Fatalf("expected the explicit model path %q, got %v", customModel, got)
	}
}

func TestTranscribePlanRejectsUnknownLanguage(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	_, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, map[string]any{
		"language": "fr",
	}))
	if planErr == nil || planErr.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("expected a validation error, got %v", planErr)
	}
	if !strings.Contains(planErr.Message, "auto, en, or es") {
		t.Fatalf("unexpected message: %q", planErr.Message)
	}
}

func TestTranscribePlanParsesFormatLists(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	cases := []struct {
		name        string
		formats     any
		wantFormats string
	}{
		{name: "any with duplicates", formats: []any{"TXT", "srt", "srt"}, wantFormats: "srt,txt"},
		{name: "string list", formats: []string{"vtt"}, wantFormats: "vtt"},
		{name: "all formats", formats: []any{"txt", "vtt", "srt"}, wantFormats: "srt,vtt,txt"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
			plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, map[string]any{
				"formats": testCase.formats,
			}))
			if planErr != nil {
				t.Fatalf("unexpected plan error: %v", planErr)
			}

			formats, ok := plan.EffectiveOptions["formats"].([]string)
			if !ok || strings.Join(formats, ",") != testCase.wantFormats {
				t.Fatalf("expected formats %q, got %v", testCase.wantFormats, plan.EffectiveOptions["formats"])
			}
			if len(plan.Outputs) != len(strings.Split(testCase.wantFormats, ",")) {
				t.Fatalf("expected %d outputs, got %+v", len(strings.Split(testCase.wantFormats, ",")), plan.Outputs)
			}
		})
	}
}

func TestTranscribePlanRejectsInvalidFormats(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	cases := []struct {
		name    string
		formats any
	}{
		{name: "unknown format", formats: []any{"json"}},
		{name: "non-string entry", formats: []any{1}},
		{name: "empty list", formats: []string{}},
		{name: "string instead of list", formats: "srt"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
			_, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, map[string]any{
				"formats": testCase.formats,
			}))
			if planErr == nil || planErr.Code != models.ErrorCodeValidationInvalidInput {
				t.Fatalf("expected a validation error, got %v", planErr)
			}
		})
	}
}

func TestTranscribePlanMissingRuntime(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	cases := []struct {
		name           string
		whisperPresent bool
		modelPresent   bool
		wantDetailCode string
	}{
		{name: "missing binary", whisperPresent: false, modelPresent: true, wantDetailCode: engine.ErrorCodeTranscribeRuntimeUnavailable},
		{name: "missing model", whisperPresent: true, modelPresent: false, wantDetailCode: engine.ErrorCodeTranscribeModelMissing},
		{name: "missing both", whisperPresent: false, modelPresent: false, wantDetailCode: engine.ErrorCodeTranscribeRuntimeUnavailable},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, testCase.whisperPresent, testCase.modelPresent))
			_, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, nil))
			if planErr == nil {
				t.Fatal("expected a plan failure")
			}
			if planErr.Code != models.ErrorCodeRuntimeDepMissing {
				t.Fatalf("expected %s, got %s", models.ErrorCodeRuntimeDepMissing, planErr.Code)
			}
			if planErr.DetailCode != testCase.wantDetailCode {
				t.Fatalf("expected detail code %s, got %s", testCase.wantDetailCode, planErr.DetailCode)
			}
			if !strings.Contains(planErr.Message, "Settings") || !strings.Contains(planErr.Message, "unavailable") {
				t.Fatalf("expected actionable guidance, got %q", planErr.Message)
			}
			if planErr.Details["whisperPath"] == nil || planErr.Details["modelPath"] == nil {
				t.Fatalf("expected resolved paths in details, got %v", planErr.Details)
			}
		})
	}
}

func TestTranscribePlanCollisionSafe(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	writeVideoFixture(t, filepath.Join(dir, "clip.srt"))
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	plan, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, nil))
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}

	srtOutput := plannedTranscribeOutput(t, plan, inputPath, "srt")
	if srtOutput.OutputPath != filepath.Join(dir, "clip-2.srt") {
		t.Fatalf("expected clip-2.srt, got %q", srtOutput.OutputPath)
	}
	if !srtOutput.Adjusted {
		t.Fatal("expected the SRT output to be marked as adjusted")
	}
	if len(srtOutput.Warnings) != 1 || !strings.Contains(srtOutput.Warnings[0], "already exists") {
		t.Fatalf("expected a collision warning, got %v", srtOutput.Warnings)
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected the plan warnings to include the collision")
	}

	vttOutput := plannedTranscribeOutput(t, plan, inputPath, "vtt")
	if vttOutput.OutputPath != filepath.Join(dir, "clip.vtt") || vttOutput.Adjusted {
		t.Fatalf("expected a natural VTT output, got %+v", vttOutput)
	}
}

func TestTranscribePlanBatchOutputs(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	firstInput := filepath.Join(firstDir, "clip.mp4")
	secondInput := filepath.Join(secondDir, "clip.mkv")
	writeVideoFixture(t, firstInput)
	writeVideoFixture(t, secondInput)
	binaryPath, modelPath := writeTranscribeDeps(t, t.TempDir())
	outputDir := t.TempDir()

	request := videoToolRequest(ToolIDVideoTranscribe, "batch", []string{firstInput, secondInput}, nil)
	request.OutputDir = outputDir

	tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("unexpected plan error: %v", planErr)
	}
	if len(plan.Outputs) != 6 {
		t.Fatalf("expected 6 outputs, got %d", len(plan.Outputs))
	}

	seen := make(map[string]struct{}, len(plan.Outputs))
	for _, planned := range plan.Outputs {
		if filepath.Dir(planned.OutputPath) != outputDir {
			t.Fatalf("expected outputs in %q, got %q", outputDir, planned.OutputPath)
		}
		if _, duplicate := seen[planned.OutputPath]; duplicate {
			t.Fatalf("duplicate planned output %q", planned.OutputPath)
		}
		seen[planned.OutputPath] = struct{}{}
	}

	firstSRT := plannedTranscribeOutput(t, plan, firstInput, "srt")
	if firstSRT.OutputPath != filepath.Join(outputDir, "clip.srt") {
		t.Fatalf("expected clip.srt for the first input, got %q", firstSRT.OutputPath)
	}
	secondSRT := plannedTranscribeOutput(t, plan, secondInput, "srt")
	if secondSRT.OutputPath != filepath.Join(outputDir, "clip-2.srt") {
		t.Fatalf("expected clip-2.srt for the second input, got %q", secondSRT.OutputPath)
	}
	if !secondSRT.Adjusted {
		t.Fatal("expected the second input to be adjusted")
	}

	if !strings.Contains(plan.Message, "2 files") {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}
}

func TestTranscribePlanRejectsMissingOutputDir(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	request := videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, nil)
	request.OutputDir = filepath.Join(dir, "missing")

	tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	_, planErr := tool.Plan(context.Background(), request)
	if planErr == nil || planErr.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("expected a validation error, got %v", planErr)
	}
}

func TestTranscribePlanRejectsMissingInputFile(t *testing.T) {
	dir := t.TempDir()
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	tool := NewTranscribeToolWithDeps(nil, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))
	_, planErr := tool.Plan(context.Background(), videoToolRequest(ToolIDVideoTranscribe, "single", []string{filepath.Join(dir, "missing.mp4")}, nil))
	if planErr == nil || planErr.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("expected a validation error, got %v", planErr)
	}
	if !strings.Contains(planErr.Message, "input does not exist") {
		t.Fatalf("unexpected message: %q", planErr.Message)
	}
}

// fakeToolTranscribeRunner replaces ffmpeg and whisper-cli for tool execution
// tests. It writes the same fixture transcript for every input.
type fakeToolTranscribeRunner struct {
	mu                 sync.Mutex
	calls              []string
	ffmpegErr          error
	writeNothing       bool
	failOnWhisperCall  int
	whisperCalls       int
	transcriberContent map[string]string
}

func newFakeToolTranscribeRunner() *fakeToolTranscribeRunner {
	return &fakeToolTranscribeRunner{
		transcriberContent: map[string]string{
			"srt": "1\n00:00:00,000 --> 00:00:02,000\nHello world\n\n2\n00:00:02,000 --> 00:00:04,000\nSecond line\n",
			"vtt": "WEBVTT\n\n00:00:00.000 --> 00:00:02.000\nHello world\n\n00:00:02.000 --> 00:00:04.000\nSecond line\n",
			"txt": "Hello world\nSecond line\n",
		},
	}
}

func (r *fakeToolTranscribeRunner) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	ffmpegErr := r.ffmpegErr
	writeNothing := r.writeNothing
	failOnWhisperCall := r.failOnWhisperCall
	if name != "ffmpeg" {
		r.whisperCalls++
	}
	whisperCall := r.whisperCalls
	contents := r.transcriberContent
	r.mu.Unlock()

	if name == "ffmpeg" {
		if ffmpegErr != nil {
			return nil, ffmpegErr
		}
		if len(args) == 0 {
			return nil, errors.New("ffmpeg called without arguments")
		}
		if err := os.WriteFile(args[len(args)-1], []byte("RIFF-fake"), 0o644); err != nil {
			return nil, err
		}

		return nil, nil
	}

	if failOnWhisperCall > 0 && whisperCall == failOnWhisperCall {
		return nil, errors.New("whisper exploded")
	}
	if writeNothing {
		return nil, nil
	}

	prefix := transcribeToolFlagValue(args, "-of")
	if prefix == "" {
		return nil, errors.New("whisper called without -of")
	}
	for _, flag := range args {
		format := ""
		switch flag {
		case "-osrt":
			format = "srt"
		case "-ovtt":
			format = "vtt"
		case "-otxt":
			format = "txt"
		default:
			continue
		}
		if err := os.WriteFile(prefix+"."+format, []byte(contents[format]), 0o644); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func (r *fakeToolTranscribeRunner) snapshotCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string{}, r.calls...)
}

func transcribeToolFlagValue(args []string, flag string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag {
			return args[index+1]
		}
	}

	return ""
}

func TestTranscribeToolExecuteSingleWithProgress(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	runner := newFakeToolTranscribeRunner()
	tool := NewTranscribeToolWithDeps(runner, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))

	request := videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, map[string]any{
		"language": "es",
		"formats":  []string{"srt", "txt"},
	})

	var progress []models.JobProgress
	item, jobErr := tool.ExecuteSingleWithProgress(context.Background(), request, func(update models.JobProgress) {
		progress = append(progress, update)
	})
	if jobErr != nil {
		t.Fatalf("unexpected execution error: %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %+v", item)
	}

	wantSRT := filepath.Join(dir, "clip.srt")
	wantTXT := filepath.Join(dir, "clip.txt")
	if item.OutputPath != wantSRT {
		t.Fatalf("expected the first output to be %q, got %q", wantSRT, item.OutputPath)
	}
	if strings.Join(item.Outputs, ",") != wantSRT+","+wantTXT {
		t.Fatalf("unexpected outputs: %v", item.Outputs)
	}
	if item.OutputCount != 2 {
		t.Fatalf("expected 2 outputs, got %d", item.OutputCount)
	}
	if item.Message != "Transcribed: 2 segments, 20 characters." {
		t.Fatalf("unexpected message: %q", item.Message)
	}

	for _, outputPath := range item.Outputs {
		info, err := os.Stat(outputPath)
		if err != nil {
			t.Fatalf("expected the output %q: %v", outputPath, err)
		}
		if info.Size() == 0 {
			t.Fatalf("expected a non-empty output %q", outputPath)
		}
	}

	whisperCall := ""
	for _, call := range runner.snapshotCalls() {
		if strings.HasPrefix(call, binaryPath+" ") {
			whisperCall = call
		}
	}
	if whisperCall == "" {
		t.Fatal("expected a whisper invocation")
	}
	if !strings.Contains(whisperCall, "-l es") {
		t.Fatalf("expected -l es, got %q", whisperCall)
	}
	if strings.Contains(whisperCall, "-ovtt") {
		t.Fatalf("expected no VTT flag, got %q", whisperCall)
	}

	wantStages := []string{"extract", "transcribe", "publish", "transcribe.done"}
	if len(progress) != len(wantStages) {
		t.Fatalf("expected %d progress updates, got %+v", len(wantStages), progress)
	}
	for index, stage := range wantStages {
		if progress[index].Stage != stage {
			t.Fatalf("expected stage %q at %d, got %q", stage, index, progress[index].Stage)
		}
	}
	if progress[len(progress)-1].Current != 100 || progress[len(progress)-1].Total != 100 {
		t.Fatalf("expected a completed final progress, got %+v", progress[len(progress)-1])
	}
}

func TestTranscribeToolExecuteSingleEmptySpeechWarning(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "clip.mp4")
	writeVideoFixture(t, inputPath)
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	runner := newFakeToolTranscribeRunner()
	runner.writeNothing = true
	tool := NewTranscribeToolWithDeps(runner, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))

	request := videoToolRequest(ToolIDVideoTranscribe, "single", []string{inputPath}, map[string]any{
		"formats": []string{"vtt"},
	})
	item, jobErr := tool.ExecuteSingle(context.Background(), request)
	if jobErr != nil {
		t.Fatalf("unexpected execution error: %v", jobErr)
	}
	if !strings.Contains(item.Message, engine.TranscriptionWarningNoSpeech) {
		t.Fatalf("expected the no-speech warning, got %q", item.Message)
	}
	if !strings.Contains(item.Message, "0 segments, 0 characters") {
		t.Fatalf("expected empty counts, got %q", item.Message)
	}

	content, err := os.ReadFile(item.OutputPath)
	if err != nil {
		t.Fatalf("read vtt output: %v", err)
	}
	if string(content) != "WEBVTT\n\n" {
		t.Fatalf("unexpected empty VTT content: %q", string(content))
	}
}

func TestTranscribeToolExecuteBatch(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	firstInput := filepath.Join(firstDir, "first.mp4")
	secondInput := filepath.Join(secondDir, "second.mp3")
	writeVideoFixture(t, firstInput)
	writeVideoFixture(t, secondInput)
	binaryPath, modelPath := writeTranscribeDeps(t, t.TempDir())
	outputDir := t.TempDir()

	runner := newFakeToolTranscribeRunner()
	tool := NewTranscribeToolWithDeps(runner, newStubTranscribeCapabilities(binaryPath, modelPath, true, true))

	request := videoToolRequest(ToolIDVideoTranscribe, "batch", []string{firstInput, secondInput}, nil)
	request.OutputDir = outputDir

	var progress []models.JobProgress
	items, jobErr := tool.ExecuteBatch(context.Background(), request, func(update models.JobProgress) {
		progress = append(progress, update)
	})
	if jobErr != nil {
		t.Fatalf("unexpected batch error: %v", jobErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	for index, item := range items {
		if !item.Success {
			t.Fatalf("expected a successful item at %d, got %+v", index, item)
		}
		if item.OutputCount != 3 || len(item.Outputs) != 3 {
			t.Fatalf("expected 3 outputs per item, got %+v", item)
		}
		if item.Message != "Transcribed: 2 segments, 20 characters." {
			t.Fatalf("unexpected message: %q", item.Message)
		}
		for _, outputPath := range item.Outputs {
			if _, err := os.Stat(outputPath); err != nil {
				t.Fatalf("expected the output %q: %v", outputPath, err)
			}
		}
	}

	if items[0].OutputPath != filepath.Join(outputDir, "first.srt") {
		t.Fatalf("unexpected first output: %q", items[0].OutputPath)
	}
	if items[1].OutputPath != filepath.Join(outputDir, "second.srt") {
		t.Fatalf("unexpected second output: %q", items[1].OutputPath)
	}

	if len(progress) != 2 {
		t.Fatalf("expected 2 batch progress updates, got %+v", progress)
	}
	if progress[0].Current != 1 || progress[0].Total != 2 || progress[1].Current != 2 || progress[1].Total != 2 {
		t.Fatalf("unexpected batch progress: %+v", progress)
	}
	if !strings.Contains(progress[0].Message, "processed 1/2") {
		t.Fatalf("unexpected batch message: %q", progress[0].Message)
	}
}

func TestTranscribeToolExecuteBatchPartialFailure(t *testing.T) {
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

	request := videoToolRequest(ToolIDVideoTranscribe, "batch", []string{firstInput, secondInput}, nil)
	request.OutputDir = outputDir

	items, jobErr := tool.ExecuteBatch(context.Background(), request, nil)
	if jobErr == nil || jobErr.Code != models.ErrorCodeExecIOTransient {
		t.Fatalf("expected an execution error, got %v", jobErr)
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
	if items[1].Error.DetailCode != engine.ErrorCodeTranscribeFailed {
		t.Fatalf("expected detail code %s, got %q", engine.ErrorCodeTranscribeFailed, items[1].Error.DetailCode)
	}
	for _, outputPath := range items[1].Outputs {
		if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("expected no output for the failed input, got %q", outputPath)
		}
	}
}

func TestMapTranscriptionErrorMappings(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		wantCode string
	}{
		{name: "runtime unavailable", code: engine.ErrorCodeTranscribeRuntimeUnavailable, wantCode: models.ErrorCodeRuntimeDepMissing},
		{name: "model missing", code: engine.ErrorCodeTranscribeModelMissing, wantCode: models.ErrorCodeRuntimeDepMissing},
		{name: "validation", code: engine.ErrorCodeTranscribeValidation, wantCode: models.ErrorCodeValidationInvalidInput},
		{name: "cancelled", code: engine.ErrorCodeTranscribeCancelled, wantCode: models.ErrorCodeCancelledByUser},
		{name: "audio failed", code: engine.ErrorCodeTranscribeAudioFailed, wantCode: models.ErrorCodeExecIOTransient},
		{name: "transcribe failed", code: engine.ErrorCodeTranscribeFailed, wantCode: models.ErrorCodeExecIOTransient},
		{name: "output failed", code: engine.ErrorCodeTranscribeOutputFailed, wantCode: models.ErrorCodeExecIOTransient},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			jobErr := mapTranscriptionError(&engine.TranscriptionError{Code: testCase.code, Message: "boom"})
			if jobErr == nil {
				t.Fatal("expected a job error")
			}
			if jobErr.Code != testCase.wantCode {
				t.Fatalf("expected %s, got %s", testCase.wantCode, jobErr.Code)
			}
			if jobErr.DetailCode != testCase.code {
				t.Fatalf("expected detail code %s, got %s", testCase.code, jobErr.DetailCode)
			}
		})
	}

	if jobErr := mapTranscriptionError(nil); jobErr != nil {
		t.Fatalf("expected nil for a nil error, got %+v", jobErr)
	}
	if jobErr := mapTranscriptionError(errors.New("plain")); jobErr == nil || jobErr.Code != "EXECUTION_ERROR" {
		t.Fatalf("expected a generic execution error, got %+v", jobErr)
	}
}

func TestTranscriptionCapabilitiesReportsProvidedRuntime(t *testing.T) {
	dir := t.TempDir()
	binaryPath, modelPath := writeTranscribeDeps(t, dir)

	response := TranscriptionCapabilities(binaryPath, modelPath)
	if !response.Success || !response.Available {
		t.Fatalf("expected an available runtime, got %+v", response)
	}
	if !response.WhisperPresent || response.WhisperPath != binaryPath {
		t.Fatalf("unexpected whisper state: %+v", response)
	}
	if !response.ModelPresent || response.ModelPath != modelPath {
		t.Fatalf("unexpected model state: %+v", response)
	}
	if response.ModelName != "ggml-base.bin" {
		t.Fatalf("unexpected model name: %q", response.ModelName)
	}
	if response.ModelSizeBytes != 5 {
		t.Fatalf("unexpected model size: %d", response.ModelSizeBytes)
	}
	if strings.Join(response.Languages, ",") != "auto,en,es" {
		t.Fatalf("unexpected languages: %v", response.Languages)
	}
	if response.DefaultLanguage != "auto" {
		t.Fatalf("unexpected default language: %q", response.DefaultLanguage)
	}
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}
