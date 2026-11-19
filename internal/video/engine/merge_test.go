package engine

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mergeTestRequest(t *testing.T) MergeRequest {
	t.Helper()

	dir := t.TempDir()
	return MergeRequest{
		InputPaths:    []string{filepath.Join(dir, "first.mp4"), filepath.Join(dir, "second.mp4")},
		OutputPath:    filepath.Join(dir, "merged.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		MergeMode:     MergeModeReencode,
	}
}

func writeMergeFixture(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

// mergeValidationRequest returns a request whose inputs exist, so validation
// reaches the option checks.
func mergeValidationRequest(t *testing.T) MergeRequest {
	t.Helper()

	req := mergeTestRequest(t)
	for _, inputPath := range req.InputPaths {
		writeMergeFixture(t, inputPath)
	}
	return req
}

type stubMergeRuntimeProbe struct{}

func (stubMergeRuntimeProbe) Check(context.Context) error {
	return nil
}

type stubMergeRunner struct {
	calls [][]string
}

func (r *stubMergeRunner) Run(_ context.Context, name string, args []string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	if len(args) == 0 {
		return nil
	}

	return os.WriteFile(args[len(args)-1], []byte("fixture"), 0o644)
}

// stubMergeDuration simulates probed clip durations so crossfade tests never
// depend on ffprobe or on real video files.
func stubMergeDuration(t *testing.T, duration float64) {
	t.Helper()

	original := MergeInputDurationProbe
	t.Cleanup(func() {
		MergeInputDurationProbe = original
	})

	MergeInputDurationProbe = func(context.Context, string) (float64, bool) {
		return duration, true
	}
}

func argValueAfter(args []string, flag string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag {
			return args[index+1]
		}
	}
	return ""
}

func TestBuildMergeArgsKeepsAudioByDefault(t *testing.T) {
	req := mergeTestRequest(t)
	listFilePath := filepath.Join(t.TempDir(), "list.txt")

	args, buildErr := BuildMergeArgs(listFilePath, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-f", "concat") {
		t.Fatalf("expected concat input, got %v", args)
	}
	if !containsArgSequence(args, "-c:a", "aac") {
		t.Fatalf("expected the default audio codec, got %v", args)
	}
	if containsArgSequence(args, "-an") {
		t.Fatalf("default merge must keep audio, got %v", args)
	}
	if got := args[len(args)-1]; got != req.OutputPath {
		t.Fatalf("expected output path %q last, got %q", req.OutputPath, got)
	}
}

func TestBuildMergeArgsRemoveAudioDropsAudioCodec(t *testing.T) {
	req := mergeTestRequest(t)
	req.RemoveAudio = true
	listFilePath := filepath.Join(t.TempDir(), "list.txt")

	args, buildErr := BuildMergeArgs(listFilePath, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
	if containsArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
	if got := args[len(args)-1]; got != req.OutputPath {
		t.Fatalf("expected output path %q last, got %q", req.OutputPath, got)
	}
}

func TestBuildMergeArgsWebMRemoveAudioDropsOpus(t *testing.T) {
	req := mergeTestRequest(t)
	req.TargetFormat = TargetFormatWebM
	req.OutputPath = filepath.Join(filepath.Dir(req.OutputPath), "merged.webm")
	req.RemoveAudio = true
	listFilePath := filepath.Join(t.TempDir(), "list.txt")

	args, buildErr := BuildMergeArgs(listFilePath, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	if !containsArgSequence(args, "-c:v", "libvpx-vp9") {
		t.Fatalf("expected the webm video codec, got %v", args)
	}
	if containsArgSequence(args, "libopus") {
		t.Fatalf("removeAudio must not add an opus codec, got %v", args)
	}
	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
}

func TestBuildMergeCopyArgsRemoveAudioDropsAudioStreams(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "merged.mp4")

	args := buildMergeCopyArgs("/tmp/list.txt", outputPath, true)

	if !containsArgSequence(args, "-c", "copy") {
		t.Fatalf("expected stream copy, got %v", args)
	}
	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
	if got := args[len(args)-1]; got != outputPath {
		t.Fatalf("expected output path %q last, got %q", outputPath, got)
	}
}

func TestBuildMergeArgsRejectsInvalidQualityPreset(t *testing.T) {
	req := mergeTestRequest(t)
	req.QualityPreset = "ultra"

	_, buildErr := BuildMergeArgs(filepath.Join(t.TempDir(), "list.txt"), req)
	if buildErr == nil {
		t.Fatal("expected a validation error")
	}
	if buildErr.Code != ErrorCodeVideoMergeValidation {
		t.Fatalf("expected %q, got %q", ErrorCodeVideoMergeValidation, buildErr.Code)
	}
}

func TestValidateMergeRequestTransition(t *testing.T) {
	tests := []struct {
		name       string
		transition string
		duration   float64
		wantErr    bool
	}{
		{name: "empty transition", transition: "", duration: 0},
		{name: "none transition", transition: "none", duration: 0},
		{name: "none ignores duration", transition: "none", duration: 5},
		{name: "crossfade default duration", transition: "crossfade", duration: 0},
		{name: "crossfade minimum duration", transition: "crossfade", duration: MergeTransitionDurationMin},
		{name: "crossfade maximum duration", transition: "crossfade", duration: MergeTransitionDurationMax},
		{name: "unknown transition", transition: "dissolve", wantErr: true},
		{name: "crossfade duration too short", transition: "crossfade", duration: 0.05, wantErr: true},
		{name: "crossfade duration too long", transition: "crossfade", duration: 3.5, wantErr: true},
		{name: "crossfade negative duration", transition: "crossfade", duration: -1, wantErr: true},
		{name: "crossfade NaN duration", transition: "crossfade", duration: math.NaN(), wantErr: true},
		{name: "crossfade infinite duration", transition: "crossfade", duration: math.Inf(1), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := mergeValidationRequest(t)
			req.Transition = test.transition
			req.TransitionDuration = test.duration

			validationErr := ValidateMergeRequest(req)
			if test.wantErr {
				if validationErr == nil {
					t.Fatal("expected a validation error")
				}
				if validationErr.Code != ErrorCodeVideoMergeValidation {
					t.Fatalf("expected %q, got %q", ErrorCodeVideoMergeValidation, validationErr.Code)
				}
				return
			}
			if validationErr != nil {
				t.Fatalf("expected the request to validate, got %v", validationErr)
			}
		})
	}
}

func TestBuildMergeCrossfadeArgsThreeClips(t *testing.T) {
	req := mergeTestRequest(t)
	req.Transition = MergeTransitionCrossfade
	req.TransitionDuration = 0.5
	req.InputPaths = append(req.InputPaths, filepath.Join(filepath.Dir(req.OutputPath), "third.mp4"))
	durations := []float64{10, 8, 6}

	args, buildErr := BuildMergeCrossfadeArgs(req.InputPaths, durations, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	filterComplex := argValueAfter(args, "-filter_complex")
	if filterComplex == "" {
		t.Fatalf("expected a filter_complex argument, got %v", args)
	}
	for _, inputPath := range req.InputPaths {
		if !containsArgSequence(args, "-i", inputPath) {
			t.Fatalf("expected an -i argument for %q, got %v", inputPath, args)
		}
	}
	if got := strings.Count(filterComplex, "settb=AVTB,setpts=PTS-STARTPTS"); got != 3 {
		t.Fatalf("expected 3 normalized video streams, got %d in %q", got, filterComplex)
	}
	for _, want := range []string{
		"xfade=transition=fade:duration=0.500:offset=9.500",
		"xfade=transition=fade:duration=0.500:offset=17.000",
	} {
		if !strings.Contains(filterComplex, want) {
			t.Fatalf("expected %q in the filter chain, got %q", want, filterComplex)
		}
	}
	if got := strings.Count(filterComplex, "acrossfade=d=0.500"); got != 2 {
		t.Fatalf("expected 2 acrossfade transitions, got %d in %q", got, filterComplex)
	}
	if !containsArgSequence(args, "-map", "[vout]") {
		t.Fatalf("expected the final video label to be mapped, got %v", args)
	}
	if !containsArgSequence(args, "-map", "[aout]") {
		t.Fatalf("expected the final audio label to be mapped, got %v", args)
	}
	if !containsArgSequence(args, "-c:v", "libx264") {
		t.Fatalf("expected the mp4 video codec, got %v", args)
	}
	if !containsArgSequence(args, "-c:a", "aac") {
		t.Fatalf("expected the mp4 audio codec, got %v", args)
	}
	if !containsArgSequence(args, "-movflags", "+faststart") {
		t.Fatalf("expected faststart for mp4, got %v", args)
	}
	if containsArgSequence(args, "-c", "copy") {
		t.Fatalf("crossfade must not stream copy, got %v", args)
	}
	if got := args[len(args)-1]; got != req.OutputPath {
		t.Fatalf("expected output path %q last, got %q", req.OutputPath, got)
	}
}

func TestBuildMergeCrossfadeArgsWebMRemoveAudio(t *testing.T) {
	req := mergeTestRequest(t)
	req.TargetFormat = TargetFormatWebM
	req.OutputPath = filepath.Join(filepath.Dir(req.OutputPath), "merged.webm")
	req.Transition = MergeTransitionCrossfade
	req.TransitionDuration = 1
	req.RemoveAudio = true

	args, buildErr := BuildMergeCrossfadeArgs(req.InputPaths, []float64{10, 8}, req)
	if buildErr != nil {
		t.Fatalf("expected args, got %v", buildErr)
	}

	filterComplex := argValueAfter(args, "-filter_complex")
	if strings.Contains(filterComplex, "acrossfade") {
		t.Fatalf("removeAudio must omit acrossfade, got %q", filterComplex)
	}
	if containsArgSequence(args, "-map", "[aout]") {
		t.Fatalf("removeAudio must not map audio, got %v", args)
	}
	if containsArgSequence(args, "-c:a") {
		t.Fatalf("removeAudio must not add an audio codec, got %v", args)
	}
	if !containsArgSequence(args, "-an") {
		t.Fatalf("expected -an for removeAudio, got %v", args)
	}
	if !containsArgSequence(args, "-c:v", "libvpx-vp9") {
		t.Fatalf("expected the webm video codec, got %v", args)
	}
	if containsArgSequence(args, "-movflags", "+faststart") {
		t.Fatalf("webm must not use faststart, got %v", args)
	}
}

func TestBuildMergeCrossfadeArgsRejectsShortClip(t *testing.T) {
	req := mergeTestRequest(t)
	req.Transition = MergeTransitionCrossfade
	req.TransitionDuration = 1

	_, buildErr := BuildMergeCrossfadeArgs(req.InputPaths, []float64{10, 0.5}, req)
	if buildErr == nil {
		t.Fatal("expected a validation error")
	}
	if buildErr.Code != ErrorCodeVideoMergeValidation {
		t.Fatalf("expected %q, got %q", ErrorCodeVideoMergeValidation, buildErr.Code)
	}
	if !strings.Contains(buildErr.Message, "too short") || !strings.Contains(buildErr.Message, "input 2") {
		t.Fatalf("expected a clear short-clip message, got %q", buildErr.Message)
	}
}

func TestBuildMergeCrossfadeArgsRequiresOneDurationPerInput(t *testing.T) {
	req := mergeTestRequest(t)
	req.Transition = MergeTransitionCrossfade

	_, buildErr := BuildMergeCrossfadeArgs(req.InputPaths, []float64{10}, req)
	if buildErr == nil {
		t.Fatal("expected a validation error")
	}
	if buildErr.Code != ErrorCodeVideoMergeValidation {
		t.Fatalf("expected %q, got %q", ErrorCodeVideoMergeValidation, buildErr.Code)
	}
}

func TestBuildMergeCrossfadeArgsRequiresTwoInputs(t *testing.T) {
	req := mergeTestRequest(t)
	req.Transition = MergeTransitionCrossfade
	req.InputPaths = req.InputPaths[:1]

	_, buildErr := BuildMergeCrossfadeArgs(req.InputPaths, []float64{10}, req)
	if buildErr == nil {
		t.Fatal("expected a validation error")
	}
	if buildErr.Code != ErrorCodeVideoMergeInsufficientInputs {
		t.Fatalf("expected %q, got %q", ErrorCodeVideoMergeInsufficientInputs, buildErr.Code)
	}
}

func TestMergeCrossfadeForcesReencode(t *testing.T) {
	req := mergeValidationRequest(t)
	req.MergeMode = MergeModeCopy
	req.Transition = MergeTransitionCrossfade
	req.TransitionDuration = 1

	runner := &stubMergeRunner{}
	stubMergeDuration(t, 10)

	if mergeErr := Merge(context.Background(), stubMergeRuntimeProbe{}, runner, req); mergeErr != nil {
		t.Fatalf("expected crossfade merge to succeed, got %v", mergeErr)
	}

	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	args := runner.calls[0]
	filterComplex := argValueAfter(args, "-filter_complex")
	if !containsArgSequence(args, "-filter_complex") || !strings.Contains(filterComplex, "xfade=transition=fade") {
		t.Fatalf("expected a crossfade filter chain, got %v", args)
	}
	if containsArgSequence(args, "-c", "copy") {
		t.Fatalf("crossfade must not use stream copy, got %v", args)
	}
	tempOutput := args[len(args)-1]
	if tempOutput == req.OutputPath || filepath.Dir(tempOutput) != filepath.Dir(req.OutputPath) || filepath.Ext(tempOutput) != filepath.Ext(req.OutputPath) {
		t.Fatalf("expected a sibling temp output for %q, got %q", req.OutputPath, tempOutput)
	}
	if _, statErr := os.Stat(req.OutputPath); statErr != nil {
		t.Fatalf("expected the published output %q: %v", req.OutputPath, statErr)
	}
}

func TestMergeCrossfadeFailsWhenDurationIsUnavailable(t *testing.T) {
	req := mergeValidationRequest(t)
	req.Transition = MergeTransitionCrossfade

	original := MergeInputDurationProbe
	t.Cleanup(func() {
		MergeInputDurationProbe = original
	})
	MergeInputDurationProbe = func(context.Context, string) (float64, bool) {
		return 0, false
	}

	runner := &stubMergeRunner{}
	mergeErr := Merge(context.Background(), stubMergeRuntimeProbe{}, runner, req)
	if mergeErr == nil {
		t.Fatal("expected a duration probe error")
	}
	if mergeErr.Code != ErrorCodeVideoMergeValidation {
		t.Fatalf("expected %q, got %q", ErrorCodeVideoMergeValidation, mergeErr.Code)
	}
	if !strings.Contains(mergeErr.Message, req.InputPaths[0]) {
		t.Fatalf("expected the message to name the input, got %q", mergeErr.Message)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("expected no ffmpeg call, got %d", len(runner.calls))
	}
}

func TestMergeWithoutTransitionKeepsConcatCopyAndSkipsProbe(t *testing.T) {
	req := mergeValidationRequest(t)
	req.MergeMode = MergeModeCopy

	probeCalled := false
	original := MergeInputDurationProbe
	t.Cleanup(func() {
		MergeInputDurationProbe = original
	})
	MergeInputDurationProbe = func(context.Context, string) (float64, bool) {
		probeCalled = true
		return 10, true
	}

	runner := &stubMergeRunner{}
	if mergeErr := Merge(context.Background(), stubMergeRuntimeProbe{}, runner, req); mergeErr != nil {
		t.Fatalf("expected merge copy to succeed, got %v", mergeErr)
	}

	if probeCalled {
		t.Fatal("no-transition merges must not probe durations")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}
	args := runner.calls[0]
	if !containsArgSequence(args, "-c", "copy") || !containsArgSequence(args, "-f", "concat") {
		t.Fatalf("expected the concat copy path, got %v", args)
	}
}
