package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTrimRequestRules(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	notDirPath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(notDirPath, []byte("not dir"), 0o644); err != nil {
		t.Fatalf("write not dir fixture: %v", err)
	}

	tests := []struct {
		name     string
		req      TrimRequest
		wantCode string
	}{
		{
			name:     "invalid time range",
			req:      TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 12, EndTime: 10, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoTrimInvalidTimeRange,
		},
		{
			name:     "target format mismatch",
			req:      TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.webm"), StartTime: 1, EndTime: 4, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoTrimFormatMismatch,
		},
		{
			name: "output exists",
			req: func() TrimRequest {
				out := filepath.Join(tmpDir, "exists.mp4")
				_ = os.WriteFile(out, []byte("exists"), 0o644)
				return TrimRequest{InputPath: inputPath, OutputPath: out, StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium}
			}(),
			wantCode: ErrorCodeVideoTrimOutputExists,
		},
		{
			name:     "output dir not found",
			req:      TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "missing", "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoTrimOutputDirNotFound,
		},
		{
			name:     "output dir not directory",
			req:      TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(notDirPath, "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoTrimOutputDirNotDirectory,
		},
		{
			name:     "output collides with input",
			req:      TrimRequest{InputPath: inputPath, OutputPath: inputPath, StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoTrimOutputCollides,
		},
		{
			name:     "invalid trim mode",
			req:      TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium, TrimMode: "turbo"},
			wantCode: ErrorCodeVideoTrimModeInvalid,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTrimRequest(tt.req)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s", tt.wantCode, err.Code)
			}
		})
	}
}

func TestTrimAutoFallbackSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeCommandRunner{errs: []error{errors.New("generic copy failure"), nil}}
	progress := make([]TrimProgressEvent, 0)
	err := TrimWithProgress(
		context.Background(),
		fakeRuntimeProbe{},
		runner,
		TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium, TrimMode: TrimModeAuto},
		func(evt TrimProgressEvent) {
			progress = append(progress, evt)
		},
	)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if runner.runCount != 2 {
		t.Fatalf("expected copy + fallback reencode, got %d runs", runner.runCount)
	}
	if len(progress) == 0 {
		t.Fatalf("expected progress events")
	}
	if progress[len(progress)-1].Percent != 100 {
		t.Fatalf("expected final progress 100, got %.2f", progress[len(progress)-1].Percent)
	}
}

func TestTrimCopyFailWithoutFallback(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeCommandRunner{errs: []error{errors.New("generic copy failure")}}
	err := Trim(context.Background(), fakeRuntimeProbe{}, runner, TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium, TrimMode: TrimModeCopy})
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Code != ErrorCodeVideoTrimCopyFailed {
		t.Fatalf("expected %s, got %s", ErrorCodeVideoTrimCopyFailed, err.Code)
	}
	if runner.runCount != 1 {
		t.Fatalf("copy mode should run once, got %d", runner.runCount)
	}
}

func TestTrimAutoFallbackFailFinal(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeCommandRunner{errs: []error{errors.New("generic copy failure"), errors.New("permission denied")}}
	err := Trim(context.Background(), fakeRuntimeProbe{}, runner, TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium, TrimMode: TrimModeAuto})
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Code != ErrorCodeVideoTrimAutoFallbackFailed {
		t.Fatalf("expected %s, got %s", ErrorCodeVideoTrimAutoFallbackFailed, err.Code)
	}
	if runner.runCount != 2 {
		t.Fatalf("auto mode should run copy + reencode, got %d", runner.runCount)
	}
}

func TestIsTrimFallbackEligible(t *testing.T) {
	if !IsTrimFallbackEligible(&VideoError{Code: ErrorCodeVideoTrimExecutionFailed, Message: "fail"}) {
		t.Fatalf("expected execution failed to be fallback eligible")
	}
	if IsTrimFallbackEligible(&VideoError{Code: ErrorCodeVideoTrimInputOpenFailed, Message: "input"}) {
		t.Fatalf("expected input-open failure to be non fallback eligible")
	}
}

func TestBuildTrimArgsByFormatAndPreset(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mov")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	argsMP4, errMP4 := BuildTrimArgs(TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 1.2, EndTime: 5.8, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetHigh})
	if errMP4 != nil {
		t.Fatalf("unexpected error: %v", errMP4)
	}
	joinedMP4 := strings.Join(argsMP4, " ")
	if !strings.Contains(joinedMP4, "-ss 1.200") || !strings.Contains(joinedMP4, "-to 5.800") {
		t.Fatalf("unexpected trim times in args: %s", joinedMP4)
	}
	if !strings.Contains(joinedMP4, "libx264") {
		t.Fatalf("unexpected mp4 args: %s", joinedMP4)
	}

	argsWebM, errWebM := BuildTrimArgs(TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.webm"), StartTime: 0, EndTime: 3, TargetFormat: TargetFormatWebM, QualityPreset: QualityPresetLow})
	if errWebM != nil {
		t.Fatalf("unexpected error: %v", errWebM)
	}
	joinedWebM := strings.Join(argsWebM, " ")
	if !strings.Contains(joinedWebM, "libvpx-vp9") || !strings.Contains(joinedWebM, "libopus") {
		t.Fatalf("unexpected webm args: %s", joinedWebM)
	}
}

func TestTrimUsesRunnerWithBuiltCommand(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeCommandRunner{}
	err := Trim(context.Background(), fakeRuntimeProbe{}, runner, TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if runner.name != "ffmpeg" {
		t.Fatalf("expected ffmpeg command, got %s", runner.name)
	}
	if len(runner.args) == 0 {
		t.Fatalf("expected ffmpeg args")
	}
}

func TestTrimRuntimeUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	err := Trim(context.Background(), fakeRuntimeProbe{err: &VideoError{Code: ErrorCodeRuntimeFFmpegNotFound, Message: "ffmpeg missing"}}, &fakeCommandRunner{}, TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 0, EndTime: 2, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if err == nil {
		t.Fatalf("expected runtime error")
	}
	if err.Code != ErrorCodeRuntimeUnavailable {
		t.Fatalf("expected %s, got %s", ErrorCodeRuntimeUnavailable, err.Code)
	}
}

func TestTrimValidationAndExecutionParity(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	out := filepath.Join(tmpDir, "already.mp4")
	if err := os.WriteFile(out, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	validateErr := ValidateTrimRequest(TrimRequest{InputPath: inputPath, OutputPath: out, StartTime: 0, EndTime: 4, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}

	execErr := Trim(context.Background(), fakeRuntimeProbe{}, &fakeCommandRunner{}, TrimRequest{InputPath: inputPath, OutputPath: out, StartTime: 0, EndTime: 4, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if execErr == nil {
		t.Fatalf("expected execute error")
	}
	if validateErr.Code != execErr.Code {
		t.Fatalf("expected same code, validate=%s execute=%s", validateErr.Code, execErr.Code)
	}
}

func TestTrimMapsRunnerErrorActionableCodes(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tests := []struct {
		name      string
		runnerErr error
		wantCode  string
	}{
		{name: "input open failed", runnerErr: errors.New("No such file or directory"), wantCode: ErrorCodeVideoTrimInputOpenFailed},
		{name: "output write failed", runnerErr: errors.New("Permission denied"), wantCode: ErrorCodeVideoTrimOutputWriteFailed},
		{name: "codec unavailable", runnerErr: errors.New("Unknown encoder 'libx264'"), wantCode: ErrorCodeVideoTrimCodecUnavailable},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeCommandRunner{err: tt.runnerErr}
			err := Trim(context.Background(), fakeRuntimeProbe{}, runner, TrimRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), StartTime: 0, EndTime: 5, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium, TrimMode: TrimModeReencode})
			if err == nil {
				t.Fatalf("expected execution error")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("expected %s, got %s", tt.wantCode, err.Code)
			}
		})
	}
}
