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

func TestMergeToolExecuteHappy(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	runner := &fakeRunner{}
	tool := NewMergeToolWithDeps(fakeProbe{}, runner)

	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoMergeV1,
		Mode:       "single",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
			"mergeMode":     "auto",
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected success item")
	}
	if item.OutputPath == "" {
		t.Fatalf("expected output path")
	}
	if runner.runCount != 1 {
		t.Fatalf("expected one ffmpeg call, got %d", runner.runCount)
	}
}

func TestMergeToolValidateInsufficientInputs(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "a.mp4")
	if err := os.WriteFile(inputPath, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	tool := NewMergeToolWithDeps(fakeProbe{}, &fakeRunner{})
	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoMergeV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
			"mergeMode":     "auto",
		},
	})
	if err == nil {
		t.Fatalf("expected insufficient input error")
	}
	if err.Code != engine.ErrorCodeVideoMergeInsufficientInputs {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoMergeInsufficientInputs, err.Code)
	}
}

func TestMergeToolExecuteAutoFallbackSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	runner := &fakeRunner{errs: []error{errors.New("generic concat failure"), nil}}
	tool := NewMergeToolWithDeps(fakeProbe{}, runner)

	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoMergeV1,
		Mode:       "single",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"targetFormat":  "mp4",
			"qualityPreset": "high",
			"mergeMode":     "auto",
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected success item")
	}
	if runner.runCount != 2 {
		t.Fatalf("expected two ffmpeg calls (copy + reencode), got %d", runner.runCount)
	}
}

func TestMergeToolExecuteAutoFallbackFinalFail(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	runner := &fakeRunner{errs: []error{errors.New("generic concat failure"), errors.New("permission denied")}}
	tool := NewMergeToolWithDeps(fakeProbe{}, runner)

	_, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoMergeV1,
		Mode:       "single",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"targetFormat":  "mp4",
			"qualityPreset": "high",
			"mergeMode":     "auto",
		},
	})

	if jobErr == nil {
		t.Fatalf("expected auto fallback final failure")
	}
	if jobErr.Code != engine.ErrorCodeVideoMergeAutoFallbackFailed {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoMergeAutoFallbackFailed, jobErr.Code)
	}
	if runner.runCount != 2 {
		t.Fatalf("expected two ffmpeg calls, got %d", runner.runCount)
	}
}

func TestMergeToolValidateInvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.mp4")
	inputB := filepath.Join(tmpDir, "b.mp4")
	if err := os.WriteFile(inputA, []byte("fixture-a"), 0o644); err != nil {
		t.Fatalf("write input a: %v", err)
	}
	if err := os.WriteFile(inputB, []byte("fixture-b"), 0o644); err != nil {
		t.Fatalf("write input b: %v", err)
	}

	tool := NewMergeToolWithDeps(fakeProbe{}, &fakeRunner{})
	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDVideoMergeV1,
		Mode:       "single",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.mp4"),
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
			"mergeMode":     "turbo",
		},
	})
	if err == nil {
		t.Fatalf("expected mergeMode validation error")
	}
	if err.Code != engine.ErrorCodeVideoMergeModeInvalid {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeVideoMergeModeInvalid, err.Code)
	}
}

func TestMergeToolValidateExecuteParity(t *testing.T) {
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

	req := models.JobRequestV1{
		ToolID:     ToolIDVideoMergeV1,
		Mode:       "single",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputPath":    out,
			"targetFormat":  "mp4",
			"qualityPreset": "medium",
			"mergeMode":     "auto",
		},
	}

	tool := NewMergeToolWithDeps(fakeProbe{}, &fakeRunner{})
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

func TestVideoMergeToolAppearsInRegistryCatalog(t *testing.T) {
	r := registry.NewRegistry()
	if err := r.RegisterToolV2(NewMergeToolWithDeps(fakeProbe{}, &fakeRunner{})); err != nil {
		t.Fatalf("register merge tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDVideoMergeV1 {
			found = true
			if !entry.Manifest.SupportsSingle || entry.Manifest.SupportsBatch {
				t.Fatalf("unexpected manifest capabilities: %+v", entry.Manifest)
			}
		}
	}

	if !found {
		t.Fatalf("expected tool %s in catalog", ToolIDVideoMergeV1)
	}
}
