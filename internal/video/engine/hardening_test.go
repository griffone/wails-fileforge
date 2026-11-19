package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordingRunner records ffmpeg invocations and delegates the behaviour to run.
type recordingRunner struct {
	calls [][]string
	run   func(ctx context.Context, name string, args []string) error
}

func (r *recordingRunner) Run(ctx context.Context, name string, args []string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	if r.run == nil {
		return writeRunnerOutput(args)
	}

	return r.run(ctx, name, args)
}

func writeRunnerOutput(args []string) error {
	if len(args) == 0 {
		return nil
	}

	return os.WriteFile(args[len(args)-1], []byte("rendered"), 0o644)
}

func assertNoVideoTempOutputs(t *testing.T, dir string) {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, ".fileforge-*"))
	if err != nil {
		t.Fatalf("glob temp outputs: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("atomic temp outputs were left behind: %v", matches)
	}
}

func TestFFmpegArgBuildersIncludeGlobalFlags(t *testing.T) {
	dir := t.TempDir()
	inputPath, outputPath := writeConvertFixtures(t)
	secondInput := filepath.Join(dir, "second.mp4")
	if err := os.WriteFile(secondInput, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write second input: %v", err)
	}
	listPath := filepath.Join(dir, "list.txt")
	if err := os.WriteFile(listPath, []byte("list"), 0o644); err != nil {
		t.Fatalf("write list: %v", err)
	}

	trimRequest := TrimRequest{
		InputPath:     inputPath,
		OutputPath:    outputPath,
		StartTime:     0,
		EndTime:       1,
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		TrimMode:      TrimModeCopy,
	}
	mergeRequest := MergeRequest{
		InputPaths:         []string{inputPath, secondInput},
		OutputPath:         outputPath,
		TargetFormat:       TargetFormatMP4,
		QualityPreset:      QualityPresetMedium,
		MergeMode:          MergeModeReencode,
		Transition:         MergeTransitionCrossfade,
		TransitionDuration: 1,
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "convert", args: mustConvertArgs(t, convertTestRequest(inputPath, outputPath))},
		{name: "trim copy", args: mustTrimArgs(t, func() ([]string, *VideoError) { return BuildTrimCopyArgs(trimRequest) })},
		{name: "trim reencode", args: mustTrimArgs(t, func() ([]string, *VideoError) { return BuildTrimReencodeArgs(trimRequest) })},
		{name: "merge copy", args: buildMergeCopyArgs(listPath, outputPath, false)},
		{name: "merge reencode", args: mustMergeArgs(t, func() ([]string, *VideoError) { return BuildMergeArgs(listPath, mergeRequest) })},
		{name: "merge crossfade", args: mustMergeArgs(t, func() ([]string, *VideoError) {
			return BuildMergeCrossfadeArgs(mergeRequest.InputPaths, []float64{10, 10}, mergeRequest)
		})},
		{name: "burn", args: BuildBurnArgs(BurnRequest{InputPath: inputPath, OutputPath: outputPath}, burnSubtitleName)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.args) < len(ffmpegGlobalArgs) {
				t.Fatalf("args too short: %v", test.args)
			}
			for index, flag := range ffmpegGlobalArgs {
				if test.args[index] != flag {
					t.Fatalf("expected global ffmpeg flags at the front, got %v", test.args)
				}
			}
		})
	}
}

func mustConvertArgs(t *testing.T, req ConvertRequest) []string {
	t.Helper()

	args, err := BuildConvertArgs(req)
	if err != nil {
		t.Fatalf("build convert args: %v", err)
	}

	return args
}

func mustTrimArgs(t *testing.T, build func() ([]string, *VideoError)) []string {
	t.Helper()

	args, err := build()
	if err != nil {
		t.Fatalf("build trim args: %v", err)
	}

	return args
}

func mustMergeArgs(t *testing.T, build func() ([]string, *VideoError)) []string {
	t.Helper()

	args, err := build()
	if err != nil {
		t.Fatalf("build merge args: %v", err)
	}

	return args
}

func TestConvertPublishesTempOutputAtomically(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)
	runner := &recordingRunner{}

	if err := Convert(context.Background(), stubMergeRuntimeProbe{}, runner, convertTestRequest(inputPath, outputPath)); err != nil {
		t.Fatalf("unexpected convert error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 ffmpeg call, got %d", len(runner.calls))
	}

	args := runner.calls[0]
	tempOutput := args[len(args)-1]
	if tempOutput == outputPath {
		t.Fatal("ffmpeg must write to a temp file, never the final path")
	}
	if filepath.Dir(tempOutput) != filepath.Dir(outputPath) || filepath.Ext(tempOutput) != filepath.Ext(outputPath) {
		t.Fatalf("expected a sibling temp output with the final extension, got %q", tempOutput)
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read published output: %v", err)
	}
	if string(content) != "rendered" {
		t.Fatalf("unexpected published content: %q", content)
	}
	assertNoVideoTempOutputs(t, filepath.Dir(outputPath))
}

func TestConvertFailureRemovesTempOutput(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)
	runner := &recordingRunner{run: func(_ context.Context, _ string, args []string) error {
		if err := writeRunnerOutput(args); err != nil {
			return err
		}
		return errors.New("ffmpeg exploded")
	}}

	convertErr := Convert(context.Background(), stubMergeRuntimeProbe{}, runner, convertTestRequest(inputPath, outputPath))
	if convertErr == nil {
		t.Fatal("expected a convert error")
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed convert must not publish the final output, stat err: %v", statErr)
	}
	assertNoVideoTempOutputs(t, filepath.Dir(outputPath))
}

func TestConvertCancellationReturnsPromptlyAndCleansUp(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)
	started := make(chan struct{})
	runner := &recordingRunner{run: func(ctx context.Context, _ string, args []string) error {
		if err := writeRunnerOutput(args); err != nil {
			return err
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *VideoError, 1)
	go func() {
		done <- Convert(ctx, stubMergeRuntimeProbe{}, runner, convertTestRequest(inputPath, outputPath))
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("ffmpeg never started")
	}

	cancel()
	var convertErr *VideoError
	select {
	case convertErr = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled convert did not return promptly")
	}
	if convertErr == nil || convertErr.Code != "CANCELED" {
		t.Fatalf("expected a CANCELED error, got %+v", convertErr)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("cancelled convert must not publish the final output, stat err: %v", statErr)
	}
	assertNoVideoTempOutputs(t, filepath.Dir(outputPath))
}

func TestTrimModesPublishAtomically(t *testing.T) {
	for _, trimMode := range []string{TrimModeCopy, TrimModeReencode, TrimModeAuto} {
		t.Run(trimMode, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "clip.mp4")
			outputPath := filepath.Join(dir, "clip_trimmed.mp4")
			if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
				t.Fatalf("write input fixture: %v", err)
			}

			runner := &recordingRunner{}
			trimErr := Trim(context.Background(), stubMergeRuntimeProbe{}, runner, TrimRequest{
				InputPath:     inputPath,
				OutputPath:    outputPath,
				StartTime:     0,
				EndTime:       1,
				TargetFormat:  TargetFormatMP4,
				QualityPreset: QualityPresetMedium,
				TrimMode:      trimMode,
			})
			if trimErr != nil {
				t.Fatalf("unexpected trim error: %v", trimErr)
			}
			if _, statErr := os.Stat(outputPath); statErr != nil {
				t.Fatalf("expected the published output: %v", statErr)
			}
			assertNoVideoTempOutputs(t, dir)
		})
	}
}

func TestMergePublishesAtomically(t *testing.T) {
	req := mergeValidationRequest(t)
	req.MergeMode = MergeModeCopy
	runner := &recordingRunner{}

	if mergeErr := Merge(context.Background(), stubMergeRuntimeProbe{}, runner, req); mergeErr != nil {
		t.Fatalf("unexpected merge error: %v", mergeErr)
	}
	if _, statErr := os.Stat(req.OutputPath); statErr != nil {
		t.Fatalf("expected the published output: %v", statErr)
	}
	assertNoVideoTempOutputs(t, filepath.Dir(req.OutputPath))
}

func TestTranscribeAudioExtractionIncludesGlobalFlags(t *testing.T) {
	runner := newFakeTranscribeRunner()
	request := newTranscribeEngineFixture(t, TranscribeFormatTXT)

	if _, transcribeErr := Transcribe(context.Background(), runner, request, nil); transcribeErr != nil {
		t.Fatalf("unexpected transcription error: %v", transcribeErr)
	}

	ffmpegCall, ok := runner.firstCall("ffmpeg")
	if !ok {
		t.Fatal("expected an ffmpeg invocation")
	}
	if len(ffmpegCall.Args) < len(ffmpegGlobalArgs) {
		t.Fatalf("ffmpeg args too short: %v", ffmpegCall.Args)
	}
	for index, flag := range ffmpegGlobalArgs {
		if ffmpegCall.Args[index] != flag {
			t.Fatalf("expected global ffmpeg flags at the front, got %v", ffmpegCall.Args)
		}
	}
}

func TestBoundedTranscribeOutputCapsCapturedBytes(t *testing.T) {
	buffer := newBoundedTranscribeOutput(1024)
	payload := []byte(strings.Repeat("x", 4096))

	written, err := buffer.Write(payload)
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if written != len(payload) {
		t.Fatalf("the writer must consume every byte, got %d of %d", written, len(payload))
	}
	if got := len(buffer.Bytes()); got != 1024 {
		t.Fatalf("captured bytes = %d, want 1024", got)
	}

	buffer.AppendLine("more output")
	if got := len(buffer.Bytes()); got != 1024 {
		t.Fatalf("captured bytes grew past the cap: %d", got)
	}
}
