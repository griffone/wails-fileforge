package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

func TestCropToolExecuteSingleHappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two", "three"})

	output := filepath.Join(tmpDir, "cropped.pdf")
	tool := NewCropTool()

	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputPath": output,
			"cropPreset": engine.CropPresetSmall,
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %+v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected success item, got %+v", item)
	}
	if item.OutputPath != output {
		t.Fatalf("expected outputPath %q, got %q", output, item.OutputPath)
	}
	if item.OutputCount != 1 || len(item.Outputs) != 1 || item.Outputs[0] != output {
		t.Fatalf("expected single output result, got %+v", item)
	}
}

func TestCropToolValidateRejectsInvalidInput(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.txt")
	if err := os.WriteFile(input, []byte("not pdf"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := NewCropTool()
	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputPath": filepath.Join(tmpDir, "out.pdf"),
			"cropPreset": engine.CropPresetSmall,
		},
	})

	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != engine.ErrorCodeValidation {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeValidation, err.Code)
	}
}

func TestCropToolValidateAndExecuteParityOutputExists(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two"})

	output := filepath.Join(tmpDir, "out.pdf")
	if err := os.WriteFile(output, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputPath": output,
			"cropPreset": engine.CropPresetSmall,
		},
	}

	tool := NewCropTool()
	validateErr := tool.Validate(context.Background(), req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}
	if validateErr.Code != engine.ErrorCodeCropOutputExists {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeCropOutputExists, validateErr.Code)
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

func TestCropToolRejectsInvalidPageSelection(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two", "three"})

	tool := NewCropTool()
	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputPath":    filepath.Join(tmpDir, "out.pdf"),
			"cropPreset":    engine.CropPresetSmall,
			"pageSelection": "1,,3",
		},
	})

	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != engine.ErrorCodeCropPageSelectionBad {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeCropPageSelectionBad, err.Code)
	}
}

func TestCropToolRejectsCustomWithoutMargins(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two"})

	tool := NewCropTool()
	err := tool.Validate(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputPath": filepath.Join(tmpDir, "out.pdf"),
			"cropPreset": engine.CropPresetCustom,
		},
	})

	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != engine.ErrorCodeCropMarginsRequired {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeCropMarginsRequired, err.Code)
	}
}

func TestCropToolValidateBatchRequiresAtLeastOneInput(t *testing.T) {
	tool := NewCropTool()
	err := tool.Validate(context.Background(), models.JobRequestV1{
		Mode:       "batch",
		InputPaths: []string{},
		Options: map[string]any{
			"outputDir":  t.TempDir(),
			"cropPreset": engine.CropPresetSmall,
		},
	})

	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != engine.ErrorCodeValidation {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeValidation, err.Code)
	}
	if err.Message != "at least 1 input PDF is required" {
		t.Fatalf("unexpected message: %s", err.Message)
	}
}

func TestCropToolExecuteBatchHappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "in-a.pdf")
	inputB := filepath.Join(tmpDir, "in-b.pdf")
	writeMultiPagePDFForSplitTool(t, inputA, []string{"one", "two"})
	writeMultiPagePDFForSplitTool(t, inputB, []string{"alpha", "beta"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	tool := NewCropTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "batch",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputDir":     outputDir,
			"cropPreset":    engine.CropPresetSmall,
			"pageSelection": "",
		},
	}, nil)

	if jobErr != nil {
		t.Fatalf("expected success, got %+v", jobErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].InputPath != inputA || items[1].InputPath != inputB {
		t.Fatalf("expected stable order by request, got %+v", items)
	}

	wantA := filepath.Join(outputDir, "in-a_cropped.pdf")
	wantB := filepath.Join(outputDir, "in-b_cropped.pdf")
	if items[0].OutputPath != wantA || items[1].OutputPath != wantB {
		t.Fatalf("unexpected output paths: %+v", items)
	}

	if items[0].OutputCount != 1 || len(items[0].Outputs) != 1 || items[0].Outputs[0] != wantA {
		t.Fatalf("unexpected item0 outputs: %+v", items[0])
	}
	if items[1].OutputCount != 1 || len(items[1].Outputs) != 1 || items[1].Outputs[0] != wantB {
		t.Fatalf("unexpected item1 outputs: %+v", items[1])
	}
}

func TestCropToolValidateAndExecuteBatchParityPreexistingOutput(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	preexisting := filepath.Join(outputDir, "input_cropped.pdf")
	if err := os.WriteFile(preexisting, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write preexisting output: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "batch",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputDir":  outputDir,
			"cropPreset": engine.CropPresetSmall,
		},
	}

	tool := NewCropTool()
	validateErr := tool.Validate(context.Background(), req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}
	if validateErr.Code != engine.ErrorCodeCropOutputExists {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeCropOutputExists, validateErr.Code)
	}

	items, executeErr := tool.ExecuteBatch(context.Background(), req, nil)
	if executeErr == nil {
		t.Fatalf("expected execute error")
	}
	if executeErr.Code != validateErr.Code {
		t.Fatalf("expected parity code %s, got %s", validateErr.Code, executeErr.Code)
	}
	if len(items) != 0 {
		t.Fatalf("expected no items on plan validation error, got %+v", items)
	}
}

func TestCropToolValidateAndExecuteBatchParityCollision(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "A.pdf")
	inputB := filepath.Join(tmpDir, "a.PDF")
	writeMultiPagePDFForSplitTool(t, inputA, []string{"one", "two"})
	writeMultiPagePDFForSplitTool(t, inputB, []string{"x", "y"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       "batch",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputDir":  outputDir,
			"cropPreset": engine.CropPresetSmall,
		},
	}

	tool := NewCropTool()
	validateErr := tool.Validate(context.Background(), req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}
	if validateErr.Code != engine.ErrorCodeCropBatchOutputCollision {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeCropBatchOutputCollision, validateErr.Code)
	}

	_, executeErr := tool.ExecuteBatch(context.Background(), req, nil)
	if executeErr == nil {
		t.Fatalf("expected execute error")
	}
	if executeErr.Code != validateErr.Code {
		t.Fatalf("expected parity code %s, got %s", validateErr.Code, executeErr.Code)
	}
}
