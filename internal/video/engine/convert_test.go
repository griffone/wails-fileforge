package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRuntimeProbe struct {
	err error
}

func (p fakeRuntimeProbe) Check(context.Context) error {
	return p.err
}

type fakeCommandRunner struct {
	name     string
	args     []string
	err      error
	errs     []error
	runCount int
}

func (r *fakeCommandRunner) Run(_ context.Context, name string, args []string) error {
	r.name = name
	r.args = append([]string(nil), args...)
	r.runCount++
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		return err
	}
	return r.err
}

func (r *fakeCommandRunner) RunWithProgress(_ context.Context, name string, args []string, _ func(percent float64, rawMessage string)) error {
	r.name = name
	r.args = append([]string(nil), args...)
	r.runCount++
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		return err
	}
	return r.err
}

func TestValidateConvertRequestRules(t *testing.T) {
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
		req      ConvertRequest
		wantCode string
	}{
		{
			name:     "target format mismatch",
			req:      ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.webm"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoFormatMismatch,
		},
		{
			name: "output exists",
			req: func() ConvertRequest {
				out := filepath.Join(tmpDir, "exists.mp4")
				_ = os.WriteFile(out, []byte("exists"), 0o644)
				return ConvertRequest{InputPath: inputPath, OutputPath: out, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium}
			}(),
			wantCode: ErrorCodeVideoOutputExists,
		},
		{
			name:     "invalid quality",
			req:      ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: "ultra"},
			wantCode: ErrorCodeVideoValidation,
		},
		{
			name:     "output dir not found",
			req:      ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "missing", "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoOutputDirNotFound,
		},
		{
			name:     "output dir not directory",
			req:      ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(notDirPath, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoOutputDirNotDirectory,
		},
		{
			name:     "output collides with input",
			req:      ConvertRequest{InputPath: inputPath, OutputPath: inputPath, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
			wantCode: ErrorCodeVideoOutputCollides,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConvertRequest(tt.req)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s", tt.wantCode, err.Code)
			}
		})
	}
}

func TestValidateConvertRequestOutputDirNotWritable(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	nonWritableDir := filepath.Join(tmpDir, "non-writable")
	if err := os.Mkdir(nonWritableDir, 0o555); err != nil {
		t.Fatalf("mkdir non-writable: %v", err)
	}

	err := ValidateConvertRequest(ConvertRequest{
		InputPath:     inputPath,
		OutputPath:    filepath.Join(nonWritableDir, "out.mp4"),
		TargetFormat:  TargetFormatMP4,
		QualityPreset: QualityPresetMedium,
	})
	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != ErrorCodeVideoOutputDirNotWritable {
		t.Fatalf("expected %s, got %s", ErrorCodeVideoOutputDirNotWritable, err.Code)
	}
}

func TestBuildConvertArgsByFormatAndPreset(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mov")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	argsMP4, errMP4 := BuildConvertArgs(ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetHigh})
	if errMP4 != nil {
		t.Fatalf("unexpected error: %v", errMP4)
	}
	joinedMP4 := strings.Join(argsMP4, " ")
	if !strings.Contains(joinedMP4, "libx264") || !strings.Contains(joinedMP4, "-movflags +faststart") {
		t.Fatalf("unexpected mp4 args: %s", joinedMP4)
	}

	argsWebM, errWebM := BuildConvertArgs(ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.webm"), TargetFormat: TargetFormatWebM, QualityPreset: QualityPresetLow})
	if errWebM != nil {
		t.Fatalf("unexpected error: %v", errWebM)
	}
	joinedWebM := strings.Join(argsWebM, " ")
	if !strings.Contains(joinedWebM, "libvpx-vp9") || !strings.Contains(joinedWebM, "libopus") {
		t.Fatalf("unexpected webm args: %s", joinedWebM)
	}
}

func TestConvertUsesRunnerWithBuiltCommand(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeCommandRunner{}
	err := Convert(context.Background(), fakeRuntimeProbe{}, runner, ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
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

func TestConvertRuntimeUnavailable(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	err := Convert(context.Background(), fakeRuntimeProbe{err: &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: "missing runtime"}}, &fakeCommandRunner{}, ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if err == nil {
		t.Fatalf("expected runtime error")
	}
	if err.Code != ErrorCodeRuntimeUnavailable {
		t.Fatalf("expected %s, got %s", ErrorCodeRuntimeUnavailable, err.Code)
	}
}

func TestConvertNormalizesRuntimeErrorCode(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	err := Convert(
		context.Background(),
		fakeRuntimeProbe{err: &VideoError{Code: ErrorCodeRuntimeFFprobeNotFound, Message: "ffprobe missing"}},
		&fakeCommandRunner{},
		ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium},
	)
	if err == nil {
		t.Fatalf("expected runtime error")
	}
	if err.Code != ErrorCodeRuntimeUnavailable {
		t.Fatalf("expected %s, got %s", ErrorCodeRuntimeUnavailable, err.Code)
	}
}

func TestConvertValidationAndExecutionParity(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	out := filepath.Join(tmpDir, "already.mp4")
	if err := os.WriteFile(out, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	validateErr := ValidateConvertRequest(ConvertRequest{InputPath: inputPath, OutputPath: out, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}

	execErr := Convert(context.Background(), fakeRuntimeProbe{}, &fakeCommandRunner{}, ConvertRequest{InputPath: inputPath, OutputPath: out, TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if execErr == nil {
		t.Fatalf("expected execute error")
	}
	if validateErr.Code != execErr.Code {
		t.Fatalf("expected same code, validate=%s execute=%s", validateErr.Code, execErr.Code)
	}
}

func TestConvertWrapsRunnerError(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeCommandRunner{err: errors.New("runner failed")}
	err := Convert(context.Background(), fakeRuntimeProbe{}, runner, ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
	if err == nil {
		t.Fatalf("expected execution error")
	}
	if err.Code != ErrorCodeVideoExecutionFailed {
		t.Fatalf("expected %s, got %s", ErrorCodeVideoExecutionFailed, err.Code)
	}
}

func TestConvertMapsRunnerErrorActionableCodes(t *testing.T) {
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
		{name: "input open failed", runnerErr: errors.New("No such file or directory"), wantCode: ErrorCodeVideoInputOpenFailed},
		{name: "output write failed", runnerErr: errors.New("Permission denied"), wantCode: ErrorCodeVideoOutputWriteFailed},
		{name: "codec unavailable", runnerErr: errors.New("Unknown encoder 'libx264'"), wantCode: ErrorCodeVideoCodecUnavailable},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeCommandRunner{err: tt.runnerErr}
			err := Convert(context.Background(), fakeRuntimeProbe{}, runner, ConvertRequest{InputPath: inputPath, OutputPath: filepath.Join(tmpDir, "out.mp4"), TargetFormat: TargetFormatMP4, QualityPreset: QualityPresetMedium})
			if err == nil {
				t.Fatalf("expected execution error")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("expected %s, got %s", tt.wantCode, err.Code)
			}
		})
	}
}
