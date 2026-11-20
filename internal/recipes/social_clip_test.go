package recipes

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/video"
)

func newSocialClipTestService(t *testing.T) (*Service, *jobhistory.Store, *eventRecorder) {
	t.Helper()

	return newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, video.NewConvertTool())
	})
}

// requireRecipeVideoToolchain skips video tests when ffmpeg/ffprobe are not
// installed instead of failing the suite.
func requireRecipeVideoToolchain(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not available")
	}
}

// writeRecipeVideo generates a 160x120 testsrc clip, with an optional sine
// audio track, so recipe tests run against real media.
func writeRecipeVideo(t *testing.T, path string, seconds int, withAudio bool) {
	t.Helper()
	requireRecipeVideoToolchain(t)

	args := []string{
		"-y",
		"-f", "lavfi",
		"-i", "testsrc=size=160x120:rate=10",
	}
	if withAudio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000")
	}
	args = append(args,
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-preset", "ultrafast",
	)
	if withAudio {
		args = append(args, "-c:a", "aac", "-shortest")
	}
	args = append(args, "-t", fmt.Sprintf("%d", seconds), path)

	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate video fixture %s: %v (%s)", path, err, strings.TrimSpace(string(output)))
	}
}

func TestPlanSocialClipPlansBothEncodesAndPoster(t *testing.T) {
	service, _, _ := newSocialClipTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeRecipeVideo(t, input, 1, false)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if plan.Recipe == nil || plan.Recipe.ID != RecipeIDSocialClip {
		t.Fatalf("expected the Social clip recipe, got %+v", plan.Recipe)
	}
	if plan.OutputDir != dir {
		t.Fatalf("expected the output folder to default to the input folder %q, got %q", dir, plan.OutputDir)
	}

	want := []string{
		filepath.Join(dir, "clip-social.mp4"),
		filepath.Join(dir, "clip-social.webm"),
		filepath.Join(dir, "clip-poster.jpg"),
	}
	if len(plan.Outputs) != len(want) {
		t.Fatalf("expected %d planned outputs, got %+v", len(want), plan.Outputs)
	}
	for index, wantPath := range want {
		if plan.Outputs[index].OutputPath != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, plan.Outputs[index].OutputPath)
		}
		if plan.Outputs[index].InputPath != input || plan.Outputs[index].OutputKind != "file" {
			t.Fatalf("output %d: unexpected metadata: %+v", index, plan.Outputs[index])
		}
	}
	if plan.Message != "Ready to build 3 files from clip.mp4." {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}
	if !warningsContain(plan.Warnings, "middle of this short clip") {
		t.Fatalf("expected a short clip warning, got %+v", plan.Warnings)
	}
	if !warningsContain(plan.Warnings, "the source has no audio") {
		t.Fatalf("expected a no-audio warning, got %+v", plan.Warnings)
	}

	// Plan must not create any output.
	for _, planned := range plan.Outputs {
		if _, err := os.Stat(planned.OutputPath); !os.IsNotExist(err) {
			t.Fatalf("plan must not create %s", planned.OutputPath)
		}
	}
}

func TestPlanSocialClipRejectsInvalidRequests(t *testing.T) {
	service, _, _ := newSocialClipTestService(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	writeRecipeVideo(t, first, 1, false)
	writeRecipeVideo(t, second, 1, false)

	textFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textFile, []byte("not a video"), 0o644); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}
	brokenVideo := filepath.Join(dir, "broken.mp4")
	if err := os.WriteFile(brokenVideo, []byte("this is not a real video"), 0o644); err != nil {
		t.Fatalf("write broken video: %v", err)
	}

	cases := []struct {
		name       string
		req        models.RecipeRequest
		wantDetail string
	}{
		{
			name:       "no inputs",
			req:        models.RecipeRequest{RecipeID: RecipeIDSocialClip},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "blank input",
			req:        models.RecipeRequest{RecipeID: RecipeIDSocialClip, InputPaths: []string{"   "}},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "multiple inputs",
			req:        models.RecipeRequest{RecipeID: RecipeIDSocialClip, InputPaths: []string{first, second}},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "non video extension",
			req:        models.RecipeRequest{RecipeID: RecipeIDSocialClip, InputPaths: []string{textFile}},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "missing video",
			req:        models.RecipeRequest{RecipeID: RecipeIDSocialClip, InputPaths: []string{filepath.Join(dir, "missing.mp4")}},
			wantDetail: "RECIPE_INPUT_UNREADABLE",
		},
		{
			name:       "unreadable media",
			req:        models.RecipeRequest{RecipeID: RecipeIDSocialClip, InputPaths: []string{brokenVideo}},
			wantDetail: "RECIPE_INPUT_UNREADABLE",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := service.Plan(testCase.req)
			if plan.Success {
				t.Fatalf("expected the plan to fail, got %+v", plan)
			}
			if plan.Error == nil || plan.Error.DetailCode != testCase.wantDetail {
				t.Fatalf("expected detail code %q, got %+v", testCase.wantDetail, plan.Error)
			}
			if plan.Message == "" {
				t.Fatal("expected a human readable plan message")
			}
			if plan.Outputs == nil {
				t.Fatal("expected a non-nil outputs list on failure")
			}
		})
	}
}

func TestRunSocialClipProducesVerifiedBundle(t *testing.T) {
	service, history, recorder := newSocialClipTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeRecipeVideo(t, input, 1, true)

	sourceMetadata, ok := inspect.ProbeVideoMetadata(context.Background(), input)
	if !ok || sourceMetadata.VideoCodec != "h264" || sourceMetadata.AudioCodec != "aac" {
		t.Fatalf("unexpected source metadata: %+v ok=%v", sourceMetadata, ok)
	}

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}

	wantOutputs := []string{
		filepath.Join(dir, "clip-social.mp4"),
		filepath.Join(dir, "clip-social.webm"),
		filepath.Join(dir, "clip-poster.jpg"),
	}
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

	mp4Info := inspect.InspectFile(wantOutputs[0])
	if mp4Info.VideoCodec != "h264" || mp4Info.AudioCodec != "aac" {
		t.Fatalf("expected the MP4 to be h264/aac, got %+v", mp4Info)
	}
	if mp4Info.Width != sourceMetadata.Width || mp4Info.Height != sourceMetadata.Height {
		t.Fatalf("expected the source resolution %dx%d, got %dx%d", sourceMetadata.Width, sourceMetadata.Height, mp4Info.Width, mp4Info.Height)
	}

	webmInfo := inspect.InspectFile(wantOutputs[1])
	if webmInfo.VideoCodec != "vp9" || webmInfo.AudioCodec != "opus" {
		t.Fatalf("expected the WebM to be vp9/opus, got %+v", webmInfo)
	}
	if webmInfo.Width != sourceMetadata.Width || webmInfo.Height != sourceMetadata.Height {
		t.Fatalf("expected the source resolution %dx%d, got %dx%d", sourceMetadata.Width, sourceMetadata.Height, webmInfo.Width, webmInfo.Height)
	}

	durationTolerance := sourceMetadata.DurationSeconds*0.02 + 0.5
	for _, path := range wantOutputs[:2] {
		metadata, metadataOK := inspect.ProbeVideoMetadata(context.Background(), path)
		if !metadataOK {
			t.Fatalf("expected metadata for %s", path)
		}
		if delta := metadata.DurationSeconds - sourceMetadata.DurationSeconds; delta > durationTolerance || delta < -durationTolerance {
			t.Fatalf("expected %s to match the source duration %.2f s, got %.2f s", filepath.Base(path), sourceMetadata.DurationSeconds, metadata.DurationSeconds)
		}
	}

	posterInfo := inspect.InspectFile(wantOutputs[2])
	if posterInfo.Kind != "image" || posterInfo.Extension != "jpg" {
		t.Fatalf("expected a JPEG poster, got %+v", posterInfo)
	}
	if posterInfo.Width <= 0 || posterInfo.Height <= 0 {
		t.Fatalf("expected poster dimensions, got %+v", posterInfo)
	}

	if result.Verification == nil || !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}
	recipeChecks := map[string]int{}
	for _, output := range result.Verification.Outputs {
		for _, check := range output.Checks {
			switch check.Key {
			case socialClipExpectedVideoCodecCheck, socialClipExpectedAudioCodecCheck, socialClipSourceDurationCheck:
				recipeChecks[check.Key]++
				if !check.Passed {
					t.Fatalf("expected %s to pass, got %+v", check.Key, check)
				}
			}
		}
	}
	if recipeChecks[socialClipExpectedVideoCodecCheck] != 2 {
		t.Fatalf("expected 2 expected_video_codec checks, got %+v", recipeChecks)
	}
	if recipeChecks[socialClipExpectedAudioCodecCheck] != 2 {
		t.Fatalf("expected 2 expected_audio_codec checks, got %+v", recipeChecks)
	}
	if recipeChecks[socialClipSourceDurationCheck] != 2 {
		t.Fatalf("expected 2 source_duration checks, got %+v", recipeChecks)
	}

	if len(result.Steps) != 3 {
		t.Fatalf("expected 3 step results, got %+v", result.Steps)
	}
	for index, step := range result.Steps {
		if step.Status != recipeStepStatusSuccess {
			t.Fatalf("expected step %d to succeed, got %+v", index, step)
		}
		if len(step.Outputs) != 1 {
			t.Fatalf("expected step %d to report one output, got %+v", index, step)
		}
	}

	entries := history.List()
	if len(entries) != 1 || entries[0].JobID != run.JobID || entries[0].ToolID != socialClipHistoryToolID {
		t.Fatalf("unexpected history entries: %+v", entries)
	}
	if len(entries[0].Outputs) != len(wantOutputs) {
		t.Fatalf("expected all %d outputs recorded for undo, got %+v", len(wantOutputs), entries[0].Outputs)
	}

	undo := history.Undo(run.JobID)
	if !undo.Success || len(undo.Moved) != len(wantOutputs) {
		t.Fatalf("expected every output to be undone, got %+v", undo)
	}
	for _, output := range wantOutputs {
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("expected %s to leave its original path, stat err = %v", output, err)
		}
	}

	restore := history.Restore(run.JobID)
	if !restore.Success || len(restore.Moved) != len(wantOutputs) {
		t.Fatalf("expected every output to be restored, got %+v", restore)
	}
	for _, output := range wantOutputs {
		if _, err := os.Stat(output); err != nil {
			t.Fatalf("expected %s to be restored: %v", output, err)
		}
	}

	terminalEvent := waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEvent) bool {
		return event.Status == models.JobStatusSuccess && event.Percent == 100
	})
	if terminalEvent.JobID != run.JobID || terminalEvent.RecipeID != RecipeIDSocialClip {
		t.Fatalf("unexpected terminal event: %+v", terminalEvent)
	}
}

func TestRunSocialClipSilentShortClipSkipsAudioChecks(t *testing.T) {
	service, _, _ := newSocialClipTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "silent.mp4")
	writeRecipeVideo(t, input, 1, false)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if !warningsContain(plan.Warnings, "middle of this short clip") {
		t.Fatalf("expected the short clip warning, got %+v", plan.Warnings)
	}

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}

	posterPath := filepath.Join(dir, "silent-poster.jpg")
	info, err := os.Stat(posterPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected the poster to exist, info=%v err=%v", info, err)
	}

	if result.Verification == nil || !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}
	for _, output := range result.Verification.Outputs {
		for _, check := range output.Checks {
			if check.Key == socialClipExpectedAudioCodecCheck {
				t.Fatalf("expected no audio codec checks for a silent source, got %+v", check)
			}
		}
	}
}

func TestRunSocialClipFailureOnSecondEncodeRemovesCreatedFiles(t *testing.T) {
	failing := &failingVideoConvertTool{
		ConvertTool: video.NewConvertTool(),
		failAt:      2,
	}

	service, history, _ := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, failing)
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeRecipeVideo(t, input, 1, true)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failed, got %q (%s)", result.Status, result.Message)
	}
	if result.Error == nil {
		t.Fatal("expected a step error in the result")
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("expected no outputs for a failed run, got %+v", result.Outputs)
	}
	if result.Steps[socialWebMStepIndex].Status != models.JobStatusFailed {
		t.Fatalf("expected the WebM step to be marked failed, got %+v", result.Steps[socialWebMStepIndex])
	}
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}

	assertDirContainsOnly(t, dir, "clip.mp4")
}

func TestRunSocialClipPosterFailureRemovesCreatedFiles(t *testing.T) {
	original := socialClipPosterCommand
	t.Cleanup(func() {
		socialClipPosterCommand = original
	})
	socialClipPosterCommand = func(context.Context, string, string, float64) error {
		return errors.New("poster extraction failed for test")
	}

	service, history, _ := newSocialClipTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeRecipeVideo(t, input, 1, true)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failed, got %q (%s)", result.Status, result.Message)
	}
	if result.Steps[socialPosterStepIndex].Status != models.JobStatusFailed {
		t.Fatalf("expected the poster step to be marked failed, got %+v", result.Steps[socialPosterStepIndex])
	}
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}

	assertDirContainsOnly(t, dir, "clip.mp4")
}

func TestRunSocialClipCancelRemovesPartialOutputs(t *testing.T) {
	blocking := &blockingVideoConvertTool{
		ConvertTool: video.NewConvertTool(),
		started:     make(chan struct{}, 1),
	}

	service, _, recorder := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, blocking)
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mp4")
	writeRecipeVideo(t, input, 1, true)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the video step to start")
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
	waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEvent) bool {
		return event.Status == models.JobStatusCancelled
	})

	assertDirContainsOnly(t, dir, "clip.mp4")
}

func TestPlanSocialClipSanitizesSourceStem(t *testing.T) {
	service, _, _ := newSocialClipTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "photo & clip.mov")
	writeRecipeVideo(t, input, 1, false)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDSocialClip,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}

	want := []string{
		filepath.Join(dir, "photo-clip-social.mp4"),
		filepath.Join(dir, "photo-clip-social.webm"),
		filepath.Join(dir, "photo-clip-poster.jpg"),
	}
	if len(plan.Outputs) != len(want) {
		t.Fatalf("expected %d planned outputs, got %+v", len(want), plan.Outputs)
	}
	for index, wantPath := range want {
		if plan.Outputs[index].OutputPath != wantPath {
			t.Fatalf("output %d: expected %q, got %q", index, wantPath, plan.Outputs[index].OutputPath)
		}
	}
}

// failingVideoConvertTool delegates to the real convert tool and fails on a
// chosen ExecuteSingle call so encode cleanup can be asserted on real media.
type failingVideoConvertTool struct {
	*video.ConvertTool
	failAt int

	mu    sync.Mutex
	calls int
}

func (t *failingVideoConvertTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	t.mu.Lock()
	t.calls++
	call := t.calls
	t.mu.Unlock()

	if call == t.failAt {
		jobErr := &models.JobError{
			Code:       "VIDEO_TEST_FAILED",
			DetailCode: "VIDEO_TEST_FAILED",
			Message:    fmt.Sprintf("video conversion failed for test on call %d", call),
		}
		return models.JobResultItem{
			InputPath: firstRequestInput(req),
			Success:   false,
			Message:   jobErr.Message,
			Error:     jobErr,
		}, jobErr
	}

	return t.ConvertTool.ExecuteSingle(ctx, req)
}

// ExecutePlan mirrors the failure behavior on the engine's plan-once path,
// which the engine prefers over ExecuteSingle.
func (t *failingVideoConvertTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	t.mu.Lock()
	t.calls++
	call := t.calls
	t.mu.Unlock()

	if call == t.failAt {
		jobErr := &models.JobError{
			Code:       "VIDEO_TEST_FAILED",
			DetailCode: "VIDEO_TEST_FAILED",
			Message:    fmt.Sprintf("video conversion failed for test on call %d", call),
		}
		return failedPlanItems(plan, jobErr), jobErr
	}

	return t.ConvertTool.ExecutePlan(ctx, req, plan, onProgress)
}

// blockingVideoConvertTool blocks in ExecuteSingle until its context is
// cancelled so the Social clip cancel path is deterministic.
type blockingVideoConvertTool struct {
	*video.ConvertTool
	started chan struct{}
}

func (t *blockingVideoConvertTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	select {
	case t.started <- struct{}{}:
	default:
	}

	<-ctx.Done()

	return models.JobResultItem{
		InputPath: firstRequestInput(req),
		Success:   false,
		Message:   "video conversion canceled",
	}, &models.JobError{
		Code:       "CANCELED",
		DetailCode: "VIDEO_TEST_CANCELED",
		Message:    "video conversion canceled",
	}
}

// ExecutePlan blocks the engine's plan-once path until cancellation.
func (t *blockingVideoConvertTool) ExecutePlan(ctx context.Context, _ models.JobRequest, plan models.PlanJobResponse, _ func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	select {
	case t.started <- struct{}{}:
	default:
	}

	<-ctx.Done()

	jobErr := &models.JobError{
		Code:       "CANCELED",
		DetailCode: "VIDEO_TEST_CANCELED",
		Message:    "video conversion canceled",
	}

	return failedPlanItems(plan, jobErr), jobErr
}

// failedPlanItems returns one failed item per planned output so the engine can
// align a failed batch-style step result.
func failedPlanItems(plan models.PlanJobResponse, jobErr *models.JobError) []models.JobResultItem {
	items := make([]models.JobResultItem, len(plan.Outputs))
	for index, output := range plan.Outputs {
		items[index] = models.JobResultItem{
			InputPath:  output.InputPath,
			OutputPath: output.OutputPath,
			Success:    false,
			Message:    jobErr.Message,
			Error:      jobErr,
		}
	}

	return items
}
