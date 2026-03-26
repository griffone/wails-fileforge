package video

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/video/engine"
)

type fakeProbe struct {
	err error
}

func (p fakeProbe) Check(context.Context) error {
	return p.err
}

type fakeRunner struct {
	name      string
	args      []string
	err       error
	errs      []error
	runCount  int
	progress  []float64
	progressM []string
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string) error {
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

func (r *fakeRunner) RunWithProgress(_ context.Context, name string, args []string, onProgress func(percent float64, rawMessage string)) error {
	r.name = name
	r.args = append([]string(nil), args...)
	r.runCount++
	for i, p := range r.progress {
		msg := ""
		if i < len(r.progressM) {
			msg = r.progressM[i]
		}
		onProgress(p, msg)
	}
	if len(r.errs) > 0 {
		err := r.errs[0]
		r.errs = r.errs[1:]
		return err
	}
	return r.err
}

func TestConvertToolValidateRuntimeUnavailable(t *testing.T) {
	tests := []struct {
		name       string
		runtimeErr error
	}{
		{name: "generic runtime unavailable", runtimeErr: &engine.VideoError{Code: engine.ErrorCodeRuntimeUnavailable, Message: "runtime unavailable"}},
		{name: "ffmpeg not found mapped", runtimeErr: &engine.VideoError{Code: engine.ErrorCodeRuntimeFFmpegNotFound, Message: "ffmpeg missing"}},
		{name: "ffprobe not found mapped", runtimeErr: &engine.VideoError{Code: engine.ErrorCodeRuntimeFFprobeNotFound, Message: "ffprobe missing"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			tool := NewConvertToolWithDeps(fakeProbe{err: tt.runtimeErr}, &fakeRunner{})

			err := tool.Validate(context.Background(), models.JobRequestV1{
				ToolID:     ToolIDVideoConvertV1,
				Mode:       "single",
				InputPaths: []string{"/tmp/in.mp4"},
				Options: map[string]any{
					"outputPath":    "/tmp/out.mp4",
					"targetFormat":  "mp4",
					"qualityPreset": "medium",
				},
			})

			if err == nil {
				t.Fatalf("expected runtime error")
			}
			if err.Code != engine.ErrorCodeRuntimeUnavailable {
				t.Fatalf("expected %s, got %s", engine.ErrorCodeRuntimeUnavailable, err.Code)
			}
		})
	}
}

func TestConvertToolValidateFormatAndPathRules(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tool := NewConvertToolWithDeps(fakeProbe{}, &fakeRunner{})

	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoConvertV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.webm"),
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
		},
	})
	if err == nil {
		t.Fatalf("expected mismatch error")
	}
	if err.Code != engine.ErrorCodeVideoFormatMismatch {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoFormatMismatch, err.Code)
	}

	out := filepath.Join(tmpDir, "exists.mp4")
	if err := os.WriteFile(out, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}
	err = tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoConvertV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    out,
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
		},
	})
	if err == nil {
		t.Fatalf("expected exists error")
	}
	if err.Code != engine.ErrorCodeVideoOutputExists {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoOutputExists, err.Code)
	}
}

func TestConvertToolValidateOutputDirVariants(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	notDirPath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(notDirPath, []byte("file"), 0o644); err != nil {
		t.Fatalf("write file fixture: %v", err)
	}

	tool := NewConvertToolWithDeps(fakeProbe{}, &fakeRunner{})

	tests := []struct {
		name       string
		outputPath string
		wantCode   string
	}{
		{name: "dir not found", outputPath: filepath.Join(tmpDir, "missing", "out.mp4"), wantCode: engine.ErrorCodeVideoOutputDirNotFound},
		{name: "parent not directory", outputPath: filepath.Join(notDirPath, "out.mp4"), wantCode: engine.ErrorCodeVideoOutputDirNotDirectory},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(context.Background(), models.JobRequestV1{
				ToolID:     ToolIDVideoConvertV1,
				Mode:       "single",
				InputPaths: []string{inputPath},
				Options: map[string]any{
					"outputPath":    tt.outputPath,
					"targetFormat":  "mp4",
					"qualityPreset": "medium",
				},
			})
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("expected %s, got %s", tt.wantCode, err.Code)
			}
		})
	}
}

func TestConvertToolExecuteBuildsCommand(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mov")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeRunner{}
	tool := NewConvertToolWithDeps(fakeProbe{}, runner)

	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoConvertV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.webm"),
			"targetFormat":  "webm",
			"qualityPreset": "high",
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected success item")
	}
	if runner.name != "ffmpeg" {
		t.Fatalf("expected ffmpeg command, got %s", runner.name)
	}
	if len(runner.args) == 0 {
		t.Fatalf("expected command args")
	}
}

func TestConvertToolValidateExecuteParity(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mkv")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	out := filepath.Join(tmpDir, "exists.webm")
	if err := os.WriteFile(out, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDVideoConvertV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    out,
			"targetFormat":  "webm",
			"qualityPreset": "low",
		},
	}

	tool := NewConvertToolWithDeps(fakeProbe{}, &fakeRunner{})
	validateErr := tool.Validate(context.Background(), req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}

	item, executeErr := tool.ExecuteSingle(context.Background(), req)
	if executeErr == nil {
		t.Fatalf("expected execute error")
	}
	if executeErr.Code != validateErr.Code {
		t.Fatalf("expected parity code %s, got %s", validateErr.Code, executeErr.Code)
	}
	if item.Error == nil || item.Error.Code != validateErr.Code {
		t.Fatalf("expected item error code %s, got %+v", validateErr.Code, item.Error)
	}
}

func TestConvertToolExecuteMapsRunnerFailure(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tool := NewConvertToolWithDeps(fakeProbe{}, &fakeRunner{err: errors.New("runner failed")})
	_, executeErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoConvertV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
		},
	})

	if executeErr == nil {
		t.Fatalf("expected execute error")
	}
	if executeErr.Code != engine.ErrorCodeVideoExecutionFailed {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoExecutionFailed, executeErr.Code)
	}
}

func TestVideoConvertToolAppearsInRegistryCatalog(t *testing.T) {
	r := registry.NewRegistry()
	if err := r.RegisterToolV2(NewConvertToolWithDeps(fakeProbe{}, &fakeRunner{})); err != nil {
		t.Fatalf("register convert tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDVideoConvertV1 {
			found = true
			if !entry.Manifest.SupportsSingle || entry.Manifest.SupportsBatch {
				t.Fatalf("unexpected manifest capabilities: %+v", entry.Manifest)
			}
		}
	}

	if !found {
		t.Fatalf("expected tool %s in catalog", ToolIDVideoConvertV1)
	}
}
