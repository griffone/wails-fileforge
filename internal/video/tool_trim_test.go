package video

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/video/engine"
)

func TestTrimToolValidateHappy(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tool := NewTrimToolWithDeps(fakeProbe{}, &fakeRunner{})

	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"startTime":     1.0,
			"endTime":       3.5,
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
			"trimMode":      "auto",
		},
	})

	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestTrimToolValidateInvalidTrimMode(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tool := NewTrimToolWithDeps(fakeProbe{}, &fakeRunner{})
	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"startTime":     1.0,
			"endTime":       3.0,
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
			"trimMode":      "turbo",
		},
	})

	if err == nil {
		t.Fatalf("expected trimMode validation error")
	}
	if err.Code != engine.ErrorCodeVideoTrimModeInvalid {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoTrimModeInvalid, err.Code)
	}
}

func TestTrimToolValidateInvalidTimeRange(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tool := NewTrimToolWithDeps(fakeProbe{}, &fakeRunner{})

	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"startTime":     10.0,
			"endTime":       5.0,
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
		},
	})

	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != engine.ErrorCodeVideoTrimInvalidTimeRange {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoTrimInvalidTimeRange, err.Code)
	}
}

func TestTrimToolValidateOutputDirVariants(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	notDirPath := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(notDirPath, []byte("file"), 0o644); err != nil {
		t.Fatalf("write file fixture: %v", err)
	}

	tool := NewTrimToolWithDeps(fakeProbe{}, &fakeRunner{})

	tests := []struct {
		name       string
		outputPath string
		wantCode   string
	}{
		{name: "dir not found", outputPath: filepath.Join(tmpDir, "missing", "out.mp4"), wantCode: engine.ErrorCodeVideoTrimOutputDirNotFound},
		{name: "parent not directory", outputPath: filepath.Join(notDirPath, "out.mp4"), wantCode: engine.ErrorCodeVideoTrimOutputDirNotDirectory},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(context.Background(), models.JobRequestV1{
				ToolID:     ToolIDVideoTrimV1,
				Mode:       "single",
				InputPaths: []string{inputPath},
				Options: map[string]any{
					"outputPath":    tt.outputPath,
					"startTime":     0.0,
					"endTime":       2.0,
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

func TestTrimToolValidateOutputExistsAndCollision(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tool := NewTrimToolWithDeps(fakeProbe{}, &fakeRunner{})

	out := filepath.Join(tmpDir, "exists.mp4")
	if err := os.WriteFile(out, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}

	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    out,
			"startTime":     0.0,
			"endTime":       1.0,
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
		},
	})
	if err == nil {
		t.Fatalf("expected exists error")
	}
	if err.Code != engine.ErrorCodeVideoTrimOutputExists {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoTrimOutputExists, err.Code)
	}

	err = tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    inputPath,
			"startTime":     0.0,
			"endTime":       1.0,
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
		},
	})
	if err == nil {
		t.Fatalf("expected collision error")
	}
	if err.Code != engine.ErrorCodeVideoTrimOutputCollides {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoTrimOutputCollides, err.Code)
	}
}

func TestTrimToolValidateExecuteParity(t *testing.T) {
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
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    out,
			"startTime":     0.0,
			"endTime":       2.0,
			"targetFormat":  "webm",
			"qualityPreset": "low",
		},
	}

	tool := NewTrimToolWithDeps(fakeProbe{}, &fakeRunner{})
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

func TestTrimToolExecuteHappy(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mov")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeRunner{}
	tool := NewTrimToolWithDeps(fakeProbe{}, runner)

	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.webm"),
			"startTime":     1.0,
			"endTime":       4.0,
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

func TestTrimToolExecuteSingleWithProgressAutoFallbackSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mov")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeRunner{errs: []error{errors.New("generic ffmpeg copy failure"), nil}}
	tool := NewTrimToolWithDeps(fakeProbe{}, runner)

	progressEvents := make([]models.JobProgressV1, 0)
	item, jobErr := tool.ExecuteSingleWithProgress(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"startTime":     1.0,
			"endTime":       4.0,
			"targetFormat":  "mp4",
			"qualityPreset": "high",
			"trimMode":      "auto",
		},
	}, func(progress models.JobProgressV1) {
		progressEvents = append(progressEvents, progress)
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected success item")
	}
	if !strings.Contains(strings.ToLower(item.Message), "fallback") {
		t.Fatalf("expected fallback info in success message, got %q", item.Message)
	}
	if runner.runCount != 2 {
		t.Fatalf("expected 2 ffmpeg attempts in auto fallback, got %d", runner.runCount)
	}
	if len(progressEvents) == 0 {
		t.Fatalf("expected progress events")
	}
	if progressEvents[len(progressEvents)-1].Current != 100 {
		t.Fatalf("expected final progress to be 100, got %d", progressEvents[len(progressEvents)-1].Current)
	}
}

func TestTrimToolExecuteSingleCopyFailWithoutFallback(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mov")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeRunner{errs: []error{errors.New("generic ffmpeg copy failure")}}
	tool := NewTrimToolWithDeps(fakeProbe{}, runner)

	_, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"startTime":     1.0,
			"endTime":       4.0,
			"targetFormat":  "mp4",
			"qualityPreset": "high",
			"trimMode":      "copy",
		},
	})

	if jobErr == nil {
		t.Fatalf("expected copy failure")
	}
	if jobErr.Code != engine.ErrorCodeVideoTrimCopyFailed {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoTrimCopyFailed, jobErr.Code)
	}
	if runner.runCount != 1 {
		t.Fatalf("copy mode should not fallback, got %d runs", runner.runCount)
	}
}

func TestTrimToolExecuteSingleAutoFallbackFailFinal(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.mov")
	if err := os.WriteFile(inputPath, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeRunner{errs: []error{errors.New("generic ffmpeg copy failure"), errors.New("permission denied")}}
	tool := NewTrimToolWithDeps(fakeProbe{}, runner)

	_, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoTrimV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"startTime":     1.0,
			"endTime":       4.0,
			"targetFormat":  "mp4",
			"qualityPreset": "high",
			"trimMode":      "auto",
		},
	})

	if jobErr == nil {
		t.Fatalf("expected auto fallback failure")
	}
	if jobErr.Code != engine.ErrorCodeVideoTrimAutoFallbackFailed {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoTrimAutoFallbackFailed, jobErr.Code)
	}
	if runner.runCount != 2 {
		t.Fatalf("expected two attempts (copy + reencode), got %d", runner.runCount)
	}
}

func TestVideoTrimToolAppearsInRegistryCatalog(t *testing.T) {
	r := registry.NewRegistry()
	if err := r.RegisterToolV2(NewTrimToolWithDeps(fakeProbe{}, &fakeRunner{})); err != nil {
		t.Fatalf("register trim tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDVideoTrimV1 {
			found = true
			if !entry.Manifest.SupportsSingle || entry.Manifest.SupportsBatch {
				t.Fatalf("unexpected manifest capabilities: %+v", entry.Manifest)
			}
		}
	}

	if !found {
		t.Fatalf("expected tool %s in catalog", ToolIDVideoTrimV1)
	}
}
