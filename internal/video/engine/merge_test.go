package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMergeRequestInsufficientInputs(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	err := ValidateMergeRequest(MergeRequest{
		InputPaths:    []string{inputPath},
		OutputPath:    filepath.Join(tmpDir, "out.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		MergeMode:     MergeModeAuto,
	})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != ErrorCodeVideoMergeInsufficientInputs {
		t.Fatalf("expected %s, got %s", ErrorCodeVideoMergeInsufficientInputs, err.Code)
	}
}

func TestMergeHappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	runner := &fakeCommandRunner{}
	err := Merge(context.Background(), fakeRuntimeProbe{}, runner, MergeRequest{
		InputPaths:    []string{inputA, inputB},
		OutputPath:    filepath.Join(tmpDir, "out.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		MergeMode:     MergeModeAuto,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if runner.runCount != 1 {
		t.Fatalf("expected one ffmpeg call, got %d", runner.runCount)
	}
	if runner.name != "ffmpeg" {
		t.Fatalf("expected ffmpeg command, got %s", runner.name)
	}
	joined := strings.Join(runner.args, " ")
	if !strings.Contains(joined, "-f concat") || !strings.Contains(joined, "-c copy") {
		t.Fatalf("expected concat copy args, got: %s", joined)
	}
}

func TestMergeAutoFallbackCopyFailThenReencodeSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	runner := &fakeCommandRunner{errs: []error{errors.New("generic concat failure"), nil}}
	err := Merge(context.Background(), fakeRuntimeProbe{}, runner, MergeRequest{
		InputPaths:    []string{inputA, inputB},
		OutputPath:    filepath.Join(tmpDir, "out.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetHigh,
		MergeMode:     MergeModeAuto,
	})
	if err != nil {
		t.Fatalf("expected success with auto fallback, got %v", err)
	}
	if runner.runCount != 2 {
		t.Fatalf("expected two ffmpeg calls (copy + reencode), got %d", runner.runCount)
	}
}

func TestMergeAutoFallbackFinalFail(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	runner := &fakeCommandRunner{errs: []error{errors.New("generic concat failure"), errors.New("permission denied")}}
	err := Merge(context.Background(), fakeRuntimeProbe{}, runner, MergeRequest{
		InputPaths:    []string{inputA, inputB},
		OutputPath:    filepath.Join(tmpDir, "out.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		MergeMode:     MergeModeAuto,
	})
	if err == nil {
		t.Fatalf("expected auto fallback final failure")
	}
	if err.Code != ErrorCodeVideoMergeAutoFallbackFailed {
		t.Fatalf("expected %s, got %s", ErrorCodeVideoMergeAutoFallbackFailed, err.Code)
	}
	if runner.runCount != 2 {
		t.Fatalf("expected two ffmpeg calls, got %d", runner.runCount)
	}
}

func TestValidateMergeRequestInvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	err := ValidateMergeRequest(MergeRequest{
		InputPaths:    []string{inputA, inputB},
		OutputPath:    filepath.Join(tmpDir, "out.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		MergeMode:     "turbo",
	})
	if err == nil {
		t.Fatalf("expected merge mode invalid error")
	}
	if err.Code != ErrorCodeVideoMergeModeInvalid {
		t.Fatalf("expected %s, got %s", ErrorCodeVideoMergeModeInvalid, err.Code)
	}
}

func TestMergeValidateExecuteParity(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	out := filepath.Join(tmpDir, "exists.mp4")
	if err := os.WriteFile(out, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	req := MergeRequest{
		InputPaths:    []string{inputA, inputB},
		OutputPath:    out,
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
		MergeMode:     MergeModeAuto,
	}

	validateErr := ValidateMergeRequest(req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}

	execErr := Merge(context.Background(), fakeRuntimeProbe{}, &fakeCommandRunner{}, req)
	if execErr == nil {
		t.Fatalf("expected execute error")
	}

	if validateErr.Code != execErr.Code {
		t.Fatalf("expected parity code %s, got %s", validateErr.Code, execErr.Code)
	}
}
