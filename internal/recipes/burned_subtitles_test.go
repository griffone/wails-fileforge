package recipes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/video"
	"fileforge-desktop/internal/video/engine"
)

// burnedSubtitlesTestSRT has two 0.6 s cues separated by a 0.6 s gap, so the
// frame proof can sample both cues and a negative control. The lines are
// deliberately short: they are the hardest case for the rendered-frame proof.
const burnedSubtitlesTestSRT = `1
00:00:00,100 --> 00:00:00,700
Hola mundo

2
00:00:01,300 --> 00:00:01,900
Segunda frase
`

func newBurnedSubtitlesTestService(t *testing.T, content string) (*Service, *jobhistory.Store, *eventRecorder) {
	t.Helper()

	return newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, &fakeBurnedSubtitlesTranscribeTool{content: content})
	})
}

// stubBurnedSubtitlesBurn replaces the engine entry point for one test.
func stubBurnedSubtitlesBurn(t *testing.T, burn func(context.Context, engine.BurnCommandRunner, string, engine.BurnRequest, func(engine.BurnProgressEvent)) (engine.BurnResult, error)) {
	t.Helper()

	original := burnedSubtitlesBurn
	t.Cleanup(func() {
		burnedSubtitlesBurn = original
	})
	burnedSubtitlesBurn = burn
}

// stubBurnedSubtitlesFrames replaces the rendered-frame proof for one test.
func stubBurnedSubtitlesFrames(t *testing.T, frames func(context.Context, string, string, []srtCue, float64) (bool, string, string, []string)) {
	t.Helper()

	original := burnedSubtitlesVerifyFrames
	t.Cleanup(func() {
		burnedSubtitlesVerifyFrames = original
	})
	burnedSubtitlesVerifyFrames = frames
}

// copyBurnedSubtitlesVideo simulates a successful burn by copying the source
// video to the planned output and reporting engine progress.
func copyBurnedSubtitlesVideo(ctx context.Context, _ engine.BurnCommandRunner, _ string, req engine.BurnRequest, onProgress func(engine.BurnProgressEvent)) (engine.BurnResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.BurnResult{}, &engine.BurnError{Code: engine.ErrorCodeBurnCancelled, Message: "Burning subtitles was canceled.", Cause: err}
	}

	if onProgress != nil {
		onProgress(engine.BurnProgressEvent{Stage: engine.BurnStagePrepare, Percent: 2, Message: "Preparing subtitles…"})
		onProgress(engine.BurnProgressEvent{Stage: engine.BurnStageRender, Percent: 50, Message: "Burning subtitles… 50%"})
		onProgress(engine.BurnProgressEvent{Stage: engine.BurnStageRender, Percent: 100, Message: "Subtitles burned."})
	}

	content, err := os.ReadFile(req.InputPath)
	if err != nil {
		return engine.BurnResult{}, err
	}
	if writeErr := os.WriteFile(req.OutputPath, content, 0o644); writeErr != nil {
		return engine.BurnResult{}, writeErr
	}

	return engine.BurnResult{OutputPath: req.OutputPath}, nil
}

func TestPlanBurnedSubtitlesPlansVideoAndSidecar(t *testing.T) {
	service, _, _ := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	dir := t.TempDir()
	input := filepath.Join(dir, "talk.mp4")
	writeRecipeVideo(t, input, 2, true)

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if plan.Recipe == nil || plan.Recipe.ID != RecipeIDBurnedSubtitles || plan.Recipe.Name != "Burned-in subtitles" {
		t.Fatalf("unexpected recipe metadata: %+v", plan.Recipe)
	}
	if plan.Recipe.Category != "video" || plan.Recipe.InputKind != "video" || plan.Recipe.MaxInputs != 1 {
		t.Fatalf("unexpected recipe constraints: %+v", plan.Recipe)
	}
	if len(plan.Recipe.Steps) != burnedSubtitlesStepCount {
		t.Fatalf("expected %d steps, got %+v", burnedSubtitlesStepCount, plan.Recipe.Steps)
	}
	if plan.OutputDir != dir {
		t.Fatalf("expected the output folder to default to the input folder %q, got %q", dir, plan.OutputDir)
	}

	want := []string{
		filepath.Join(dir, "talk_subtitled.mp4"),
		filepath.Join(dir, "talk_subtitled.srt"),
	}
	if len(plan.Outputs) != len(want) {
		t.Fatalf("expected %d planned outputs, got %+v", len(want), plan.Outputs)
	}
	for index, wantPath := range want {
		output := plan.Outputs[index]
		if output.OutputPath != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, output.OutputPath)
		}
		if output.InputPath != input || output.OutputKind != "file" || output.OutputCount != 1 {
			t.Fatalf("output %d: unexpected metadata: %+v", index, output)
		}
		if output.Exists {
			t.Fatalf("output %d: expected the planned output to not exist yet: %+v", index, output)
		}
	}
	if plan.Message != "Ready to transcribe and burn subtitles into talk.mp4." {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}
	if warningsContain(plan.Warnings, "re-encoded") {
		t.Fatalf("expected no audio re-encode warning for AAC, got %+v", plan.Warnings)
	}

	for _, planned := range plan.Outputs {
		if _, err := os.Stat(planned.OutputPath); !os.IsNotExist(err) {
			t.Fatalf("plan must not create %s", planned.OutputPath)
		}
	}
}

func TestPlanBurnedSubtitlesWarnsAboutAudioReEncode(t *testing.T) {
	service, _, _ := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	dir := t.TempDir()
	input := filepath.Join(dir, "talk.mov")
	writeBurnedSubtitlesPCMVideo(t, input)

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if !warningsContain(plan.Warnings, "Audio will be re-encoded to AAC for compatibility.") {
		t.Fatalf("expected the audio re-encode warning, got %+v", plan.Warnings)
	}
}

func TestPlanBurnedSubtitlesRejectsVideoWithoutAudio(t *testing.T) {
	service, _, _ := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	dir := t.TempDir()
	input := filepath.Join(dir, "silent.mp4")
	writeRecipeVideo(t, input, 1, false)

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if plan.Success {
		t.Fatalf("expected the plan to fail, got %+v", plan)
	}
	if plan.Error == nil || plan.Error.DetailCode != "RECIPE_INPUT_NO_AUDIO" {
		t.Fatalf("expected RECIPE_INPUT_NO_AUDIO, got %+v", plan.Error)
	}
	if plan.Message == "" {
		t.Fatal("expected a human readable plan message")
	}
}

func TestPlanBurnedSubtitlesRejectsInvalidRequests(t *testing.T) {
	service, _, _ := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeRecipeVideo(t, first, 1, true)
	writeRecipeVideo(t, second, 1, true)

	textFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textFile, []byte("not a video"), 0o644); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}

	cases := []struct {
		name       string
		inputs     []string
		wantDetail string
	}{
		{name: "no inputs", wantDetail: "RECIPE_INVALID_INPUT"},
		{name: "multiple inputs", inputs: []string{first, second}, wantDetail: "RECIPE_INVALID_INPUT"},
		{name: "non video extension", inputs: []string{textFile}, wantDetail: "RECIPE_INVALID_INPUT"},
		{name: "missing video", inputs: []string{filepath.Join(dir, "missing.mp4")}, wantDetail: "RECIPE_INPUT_UNREADABLE"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := service.Plan(models.RecipeRequestV1{
				RecipeID:   RecipeIDBurnedSubtitles,
				InputPaths: testCase.inputs,
			})
			if plan.Success {
				t.Fatalf("expected the plan to fail, got %+v", plan)
			}
			if plan.Error == nil || plan.Error.DetailCode != testCase.wantDetail {
				t.Fatalf("expected detail code %q, got %+v", testCase.wantDetail, plan.Error)
			}
			if plan.Message == "" {
				t.Fatal("expected a human readable plan message")
			}
		})
	}
}

func TestRunBurnedSubtitlesProducesVideoAndSidecar(t *testing.T) {
	service, history, recorder := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	tempRoot := t.TempDir()
	service.tempDir = func() (string, error) {
		return os.MkdirTemp(tempRoot, "recipe-*")
	}

	stubBurnedSubtitlesBurn(t, copyBurnedSubtitlesVideo)
	stubBurnedSubtitlesFrames(t, func(context.Context, string, string, []srtCue, float64) (bool, string, string, []string) {
		return true, "2 of 2 cues", "cue 1 at 0.40 s: 24.00; cue 2 at 1.60 s: 23.00; control at 1.00 s: 0.10", nil
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "talk.mp4")
	writeRecipeVideo(t, input, 2, true)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}

	videoPath := filepath.Join(dir, "talk_subtitled.mp4")
	srtPath := filepath.Join(dir, "talk_subtitled.srt")
	wantOutputs := []string{videoPath, srtPath}
	if len(result.Outputs) != len(wantOutputs) {
		t.Fatalf("expected %d outputs, got %+v", len(wantOutputs), result.Outputs)
	}
	for index, wantPath := range wantOutputs {
		if result.Outputs[index] != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, result.Outputs[index])
		}
		info, err := os.Stat(wantPath)
		if err != nil || info.Size() == 0 {
			t.Fatalf("expected %s to exist and be non-empty, info=%v err=%v", wantPath, info, err)
		}
	}

	sidecarInfo, err := os.Stat(srtPath)
	if err != nil {
		t.Fatalf("stat sidecar: %v", err)
	}
	if sidecarInfo.Mode().Perm() != 0o644 {
		t.Fatalf("expected the sidecar mode 0644, got %o", sidecarInfo.Mode().Perm())
	}
	sidecarContent, err := os.ReadFile(srtPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if string(sidecarContent) != burnedSubtitlesTestSRT {
		t.Fatalf("expected the sidecar to keep the transcribed cues, got %q", string(sidecarContent))
	}

	if len(result.Steps) != burnedSubtitlesStepCount {
		t.Fatalf("expected %d step results, got %+v", burnedSubtitlesStepCount, result.Steps)
	}
	if result.Steps[burnedSubtitlesStepIndexTranscribe].Status != recipeStepStatusSuccess {
		t.Fatalf("expected the transcribe step to succeed, got %+v", result.Steps[burnedSubtitlesStepIndexTranscribe])
	}
	if result.Steps[burnedSubtitlesStepIndexTranscribe].Message != "2 cues transcribed." {
		t.Fatalf("unexpected transcribe message: %+v", result.Steps[burnedSubtitlesStepIndexTranscribe])
	}
	if len(result.Steps[burnedSubtitlesStepIndexTranscribe].Outputs) != 0 {
		t.Fatalf("expected no temp outputs in the transcribe step, got %+v", result.Steps[burnedSubtitlesStepIndexTranscribe])
	}
	if result.Steps[burnedSubtitlesStepIndexBurn].Status != recipeStepStatusSuccess {
		t.Fatalf("expected the burn step to succeed, got %+v", result.Steps[burnedSubtitlesStepIndexBurn])
	}
	if outputs := result.Steps[burnedSubtitlesStepIndexBurn].Outputs; len(outputs) != 1 || outputs[0] != videoPath {
		t.Fatalf("expected the burn step to report the video, got %+v", outputs)
	}
	if result.Steps[burnedSubtitlesStepIndexVerify].Status != recipeStepStatusSuccess {
		t.Fatalf("expected the verify step to succeed, got %+v", result.Steps[burnedSubtitlesStepIndexVerify])
	}

	if result.Verification == nil || !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}
	rendered := findBurnedSubtitlesCheck(t, result, videoPath, burnedSubtitlesRenderedCheck)
	if !rendered.Passed || rendered.Value != "2 of 2 cues" {
		t.Fatalf("expected the rendered-frame proof to pass, got %+v", rendered)
	}

	entries := history.List()
	if len(entries) != 1 || entries[0].JobID != run.JobID || entries[0].ToolID != burnedSubtitlesHistoryToolID {
		t.Fatalf("unexpected history entries: %+v", entries)
	}
	if len(entries[0].Outputs) != len(wantOutputs) {
		t.Fatalf("expected all %d outputs recorded for undo, got %+v", len(wantOutputs), entries[0].Outputs)
	}
	for index, wantPath := range wantOutputs {
		if entries[0].Outputs[index].Path != wantPath {
			t.Fatalf("history output %d: expected %q, got %q", index, wantPath, entries[0].Outputs[index].Path)
		}
	}

	terminalEvent := waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.JobID == run.JobID && event.Status == models.JobStatusSuccess && event.Percent == burnedSubtitlesProgressEnd
	})
	if terminalEvent.RecipeID != RecipeIDBurnedSubtitles {
		t.Fatalf("unexpected terminal event: %+v", terminalEvent)
	}

	stages := map[string]bool{}
	lastPercent := -1
	for _, event := range recorder.snapshot() {
		if event.JobID != run.JobID {
			continue
		}
		if event.Percent < lastPercent {
			t.Fatalf("progress went backwards: %d after %d (%+v)", event.Percent, lastPercent, event)
		}
		lastPercent = event.Percent
		stages[event.Stage] = true
	}
	for _, stage := range []string{"transcribe", "burn", "verify"} {
		if !stages[stage] {
			t.Fatalf("expected a %q progress stage, got %+v", stage, stages)
		}
	}
	if lastPercent != burnedSubtitlesProgressEnd {
		t.Fatalf("expected progress to end at %d, got %d", burnedSubtitlesProgressEnd, lastPercent)
	}

	waitForEmptyDir(t, tempRoot)
}

func TestRunBurnedSubtitlesBurnFailureRemovesCreatedFiles(t *testing.T) {
	service, history, _ := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	tempRoot := t.TempDir()
	service.tempDir = func() (string, error) {
		return os.MkdirTemp(tempRoot, "recipe-*")
	}

	stubBurnedSubtitlesBurn(t, func(context.Context, engine.BurnCommandRunner, string, engine.BurnRequest, func(engine.BurnProgressEvent)) (engine.BurnResult, error) {
		return engine.BurnResult{}, &engine.BurnError{
			Code:    engine.ErrorCodeBurnFailed,
			Message: "FFmpeg could not burn the subtitles",
			Cause:   errors.New("boom"),
		}
	})

	framesCalled := make(chan struct{}, 1)
	stubBurnedSubtitlesFrames(t, func(context.Context, string, string, []srtCue, float64) (bool, string, string, []string) {
		select {
		case framesCalled <- struct{}{}:
		default:
		}
		return true, "", "", nil
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "talk.mp4")
	writeRecipeVideo(t, input, 2, true)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failed, got %q (%s)", result.Status, result.Message)
	}
	if result.Error == nil || result.Error.DetailCode != engine.ErrorCodeBurnFailed {
		t.Fatalf("expected BURN_FAILED, got %+v", result.Error)
	}
	reason, _ := result.Error.Details["reason"].(string)
	if !strings.Contains(reason, "boom") {
		t.Fatalf("expected the bounded burn cause, got %+v", result.Error.Details)
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("expected no outputs for a failed run, got %+v", result.Outputs)
	}
	if result.Steps[burnedSubtitlesStepIndexBurn].Status != models.JobStatusFailed {
		t.Fatalf("expected the burn step to be marked failed, got %+v", result.Steps[burnedSubtitlesStepIndexBurn])
	}
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}
	select {
	case <-framesCalled:
		t.Fatal("expected the frame proof to never run after a burn failure")
	default:
	}

	assertDirContainsOnly(t, dir, "talk.mp4")
	waitForEmptyDir(t, tempRoot)
}

func TestRunBurnedSubtitlesCancelRemovesPartialOutputs(t *testing.T) {
	service, _, recorder := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	tempRoot := t.TempDir()
	service.tempDir = func() (string, error) {
		return os.MkdirTemp(tempRoot, "recipe-*")
	}

	started := make(chan struct{}, 1)
	stubBurnedSubtitlesBurn(t, func(ctx context.Context, _ engine.BurnCommandRunner, _ string, _ engine.BurnRequest, _ func(engine.BurnProgressEvent)) (engine.BurnResult, error) {
		select {
		case started <- struct{}{}:
		default:
		}

		<-ctx.Done()

		return engine.BurnResult{}, &engine.BurnError{
			Code:    engine.ErrorCodeBurnCancelled,
			Message: "Burning subtitles was canceled.",
			Cause:   ctx.Err(),
		}
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "talk.mp4")
	writeRecipeVideo(t, input, 2, true)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the burn step to start")
	}

	cancelled := service.Cancel(run.JobID)
	if !cancelled.Success || !cancelled.Found {
		t.Fatalf("expected cancel to be accepted, got %+v", cancelled)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusCancelled {
		t.Fatalf("expected cancelled, got %q (%s)", result.Status, result.Message)
	}
	if result.Error == nil || result.Error.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected a cancellation error, got %+v", result.Error)
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("expected no outputs for a cancelled run, got %+v", result.Outputs)
	}
	waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.Status == models.JobStatusCancelled
	})

	assertDirContainsOnly(t, dir, "talk.mp4")
	waitForEmptyDir(t, tempRoot)
}

func TestRunBurnedSubtitlesEmptyTranscriptionFailsWithoutBurning(t *testing.T) {
	service, history, _ := newBurnedSubtitlesTestService(t, "")

	burnCalled := make(chan struct{}, 1)
	stubBurnedSubtitlesBurn(t, func(context.Context, engine.BurnCommandRunner, string, engine.BurnRequest, func(engine.BurnProgressEvent)) (engine.BurnResult, error) {
		select {
		case burnCalled <- struct{}{}:
		default:
		}
		return engine.BurnResult{}, errors.New("burn must not run without cues")
	})
	stubBurnedSubtitlesFrames(t, func(context.Context, string, string, []srtCue, float64) (bool, string, string, []string) {
		return true, "", "", nil
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "talk.mp4")
	writeRecipeVideo(t, input, 2, true)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failed, got %q (%s)", result.Status, result.Message)
	}
	if result.Error == nil || result.Error.DetailCode != "RECIPE_NO_SPEECH" {
		t.Fatalf("expected RECIPE_NO_SPEECH, got %+v", result.Error)
	}
	if result.Steps[burnedSubtitlesStepIndexTranscribe].Status != models.JobStatusFailed {
		t.Fatalf("expected the transcribe step to be marked failed, got %+v", result.Steps[burnedSubtitlesStepIndexTranscribe])
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("expected no outputs for a failed run, got %+v", result.Outputs)
	}
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}
	select {
	case <-burnCalled:
		t.Fatal("expected the burn to be skipped when no speech was recognized")
	default:
	}

	assertDirContainsOnly(t, dir, "talk.mp4")
}

func TestRunBurnedSubtitlesIntegrationRendersFrames(t *testing.T) {
	requireRecipeVideoToolchain(t)

	service, _, _ := newBurnedSubtitlesTestService(t, burnedSubtitlesTestSRT)

	originalBurn := burnedSubtitlesBurn
	originalFrames := burnedSubtitlesVerifyFrames
	t.Cleanup(func() {
		burnedSubtitlesBurn = originalBurn
		burnedSubtitlesVerifyFrames = originalFrames
	})
	burnedSubtitlesBurn = engine.Burn
	burnedSubtitlesVerifyFrames = verifyBurnedSubtitlesFrames

	dir := t.TempDir()
	input := filepath.Join(dir, "lecture.mp4")
	writeBurnedSubtitlesFixture(t, input)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDBurnedSubtitles,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s), verification: %+v", result.Status, result.Message, result.Verification)
	}

	videoPath := filepath.Join(dir, "lecture_subtitled.mp4")
	srtPath := filepath.Join(dir, "lecture_subtitled.srt")
	if len(result.Outputs) != 2 || result.Outputs[0] != videoPath || result.Outputs[1] != srtPath {
		t.Fatalf("unexpected outputs: %+v", result.Outputs)
	}

	if result.Verification == nil || !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}

	rendered := findBurnedSubtitlesCheck(t, result, videoPath, burnedSubtitlesRenderedCheck)
	if !rendered.Passed {
		t.Fatalf("expected the rendered-frame proof to pass, got %+v", rendered)
	}
	if !strings.Contains(rendered.Value, "cues") || rendered.Detail == "" {
		t.Fatalf("expected measured frame values, got %+v", rendered)
	}
	t.Logf("rendered-frame proof: %s", rendered.Detail)

	duration := findBurnedSubtitlesCheck(t, result, videoPath, burnedSubtitlesDurationCheck)
	if !duration.Passed {
		t.Fatalf("expected the duration check to pass, got %+v", duration)
	}
	frameSize := findBurnedSubtitlesCheck(t, result, videoPath, burnedSubtitlesFrameSizeCheck)
	if !frameSize.Passed || frameSize.Value != "640 × 360 px" {
		t.Fatalf("expected the source frame size, got %+v", frameSize)
	}
	audio := findBurnedSubtitlesCheck(t, result, videoPath, burnedSubtitlesAudioCheck)
	if !audio.Passed || audio.Value != "aac" {
		t.Fatalf("expected the copied source audio, got %+v", audio)
	}
	timing := findBurnedSubtitlesCheck(t, result, srtPath, burnedSubtitlesCueTimingCheck)
	if !timing.Passed {
		t.Fatalf("expected the sidecar timing to pass, got %+v", timing)
	}
}

func TestParseSRTCues(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []srtCue
	}{
		{
			name: "standard cues with comma milliseconds",
			content: "1\n00:00:01,500 --> 00:00:02,750\nHello world\n\n" +
				"2\n00:00:03,000 --> 00:00:04,250\nSecond line\n",
			want: []srtCue{
				{Start: 1.5, End: 2.75, Text: "Hello world"},
				{Start: 3, End: 4.25, Text: "Second line"},
			},
		},
		{
			name:    "dot milliseconds",
			content: "00:00:05.500 --> 00:00:06.250\nDotted\n",
			want:    []srtCue{{Start: 5.5, End: 6.25, Text: "Dotted"}},
		},
		{
			name:    "hours may exceed 23",
			content: "25:00:00,000 --> 25:00:01,000\nLong video\n",
			want:    []srtCue{{Start: 90000, End: 90001, Text: "Long video"}},
		},
		{
			name:    "multiline text is joined and trimmed",
			content: "1\n00:00:00,000 --> 00:00:02,000\n  first line  \nsecond line\n",
			want:    []srtCue{{Start: 0, End: 2, Text: "first line   second line"}},
		},
		{
			name:    "CRLF line endings",
			content: "1\r\n00:00:00,100 --> 00:00:00,900\r\nWindows\r\n",
			want:    []srtCue{{Start: 0.1, End: 0.9, Text: "Windows"}},
		},
		{
			name:    "trailing coordinates are ignored",
			content: "1\n00:00:01,000 --> 00:00:02,000 X1:10 X2:20 Y1:30 Y2:40\nPositioned\n",
			want:    []srtCue{{Start: 1, End: 2, Text: "Positioned"}},
		},
		{
			name: "malformed blocks are skipped",
			content: "no timing here\n\n" +
				"1\n00:00:02,000 --> 00:00:01,000\nReversed\n\n" +
				"2\nnot-a-timestamp --> 00:00:03,000\nBroken\n\n" +
				"3\n00:00:04,000 --> 00:00:05,000\nKept\n",
			want: []srtCue{{Start: 4, End: 5, Text: "Kept"}},
		},
		{
			name:    "empty content",
			content: " \n\n ",
			want:    nil,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseSRTCues(testCase.content)
			if len(got) != len(testCase.want) {
				t.Fatalf("expected %d cues, got %+v", len(testCase.want), got)
			}
			for index, want := range testCase.want {
				if got[index].Text != want.Text {
					t.Fatalf("cue %d: expected text %q, got %q", index, want.Text, got[index].Text)
				}
				if diff := got[index].Start - want.Start; diff > 1e-6 || diff < -1e-6 {
					t.Fatalf("cue %d: expected start %.3f, got %.3f", index, want.Start, got[index].Start)
				}
				if diff := got[index].End - want.End; diff > 1e-6 || diff < -1e-6 {
					t.Fatalf("cue %d: expected end %.3f, got %.3f", index, want.End, got[index].End)
				}
			}
		})
	}
}

func TestSRTCueTimingValid(t *testing.T) {
	cases := []struct {
		name     string
		cues     []srtCue
		duration float64
		want     bool
	}{
		{
			name:     "valid cues",
			cues:     []srtCue{{Start: 0.1, End: 0.7}, {Start: 1.3, End: 1.9}},
			duration: 2,
			want:     true,
		},
		{
			name:     "last cue ends at the tolerance edge",
			cues:     []srtCue{{Start: 0, End: 2.5}},
			duration: 2,
			want:     true,
		},
		{
			name:     "no cues",
			cues:     nil,
			duration: 2,
			want:     false,
		},
		{
			name:     "cue ends before it starts",
			cues:     []srtCue{{Start: 1, End: 1}},
			duration: 2,
			want:     false,
		},
		{
			name:     "start times out of order",
			cues:     []srtCue{{Start: 1, End: 1.5}, {Start: 0.5, End: 0.9}},
			duration: 2,
			want:     false,
		},
		{
			name:     "last cue ends after the video",
			cues:     []srtCue{{Start: 0, End: 2.6}},
			duration: 2,
			want:     false,
		},
		{
			name:     "unknown duration",
			cues:     []srtCue{{Start: 0, End: 1}},
			duration: 0,
			want:     false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			passed, reason := srtCueTimingValid(testCase.cues, testCase.duration)
			if passed != testCase.want {
				t.Fatalf("expected passed=%v, got %v (%s)", testCase.want, passed, reason)
			}
			if !passed && reason == "" {
				t.Fatal("expected a reason for every failed timing check")
			}
			if passed && reason != "" {
				t.Fatalf("expected no reason for a valid timing check, got %q", reason)
			}
		})
	}
}

// fakeBurnedSubtitlesTranscribeTool writes a deterministic SRT instead of
// running Whisper, so recipe execution can be tested without a model.
type fakeBurnedSubtitlesTranscribeTool struct {
	content string
}

func (t *fakeBurnedSubtitlesTranscribeTool) ID() string { return video.ToolIDVideoTranscribeV1 }

func (t *fakeBurnedSubtitlesTranscribeTool) Capability() string { return t.ID() }

func (t *fakeBurnedSubtitlesTranscribeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.ID(), Name: "Fake Transcribe"}
}

func (t *fakeBurnedSubtitlesTranscribeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *fakeBurnedSubtitlesTranscribeTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *fakeBurnedSubtitlesTranscribeTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	inputPath := firstRequestInput(req)
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	outputPath := filepath.Join(req.OutputDir, stem+".srt")

	return models.PlanJobResponseV1{
		Success: true,
		Message: "ready to transcribe",
		Outputs: []models.PlannedOutputV1{{
			InputPath:   inputPath,
			OutputPath:  outputPath,
			OutputKind:  "file",
			OutputCount: 1,
		}},
	}, nil
}

func (t *fakeBurnedSubtitlesTranscribeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *fakeBurnedSubtitlesTranscribeTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	if onProgress != nil {
		onProgress(models.JobProgressV1{Current: 25, Total: 100, Stage: "transcribe", Message: "Recognizing speech."})
		onProgress(models.JobProgressV1{Current: 75, Total: 100, Stage: "transcribe", Message: "Almost there."})
	}

	plan, jobErr := t.Plan(ctx, req)
	if jobErr != nil {
		return models.JobResultItemV1{InputPath: firstRequestInput(req), Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}
	outputPath := plan.Outputs[0].OutputPath

	if writeErr := os.WriteFile(outputPath, []byte(t.content), 0o644); writeErr != nil {
		jobErr := models.NewCanonicalJobError("RECIPE_TEST_TRANSCRIBE_FAILED", "could not write the test subtitles", nil)
		return models.JobResultItemV1{InputPath: firstRequestInput(req), Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:   firstRequestInput(req),
		OutputPath:  outputPath,
		Outputs:     []string{outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "Transcribed: 2 segments.",
	}, nil
}

// writeBurnedSubtitlesPCMVideo writes a video whose audio codec cannot be
// copied into MP4, so the plan must warn about the AAC re-encode.
func writeBurnedSubtitlesPCMVideo(t *testing.T, path string) {
	t.Helper()

	requireRecipeVideoToolchain(t)

	args := []string{
		"-y",
		"-f", "lavfi", "-i", "testsrc=size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-preset", "ultrafast",
		"-c:a", "pcm_s16le",
		"-t", "2",
		path,
	}

	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate PCM video fixture %s: %v (%s)", path, err, strings.TrimSpace(string(output)))
	}
}

// writeBurnedSubtitlesFixture writes a 640x360 black clip with audio so the
// real burn and the real frame proof can run against deterministic pixels.
func writeBurnedSubtitlesFixture(t *testing.T, path string) {
	t.Helper()

	args := []string{
		"-y",
		"-f", "lavfi", "-i", "color=c=black:s=640x360:r=25",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-preset", "ultrafast",
		"-c:a", "aac",
		"-t", "2",
		path,
	}

	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate burn fixture %s: %v (%s)", path, err, strings.TrimSpace(string(output)))
	}
}

// findBurnedSubtitlesCheck returns the recipe check attached to path.
func findBurnedSubtitlesCheck(t *testing.T, result models.RecipeResultV1, path, key string) models.VerificationCheckV1 {
	t.Helper()

	if result.Verification != nil {
		for _, output := range result.Verification.Outputs {
			if canonicalInputKey(output.Path) != canonicalInputKey(path) {
				continue
			}
			for _, check := range output.Checks {
				if check.Key == key {
					return check
				}
			}
		}
	}

	t.Fatalf("expected check %q on %s, got %+v", key, path, result.Verification)
	return models.VerificationCheckV1{}
}
