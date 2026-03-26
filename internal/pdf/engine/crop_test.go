package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCropValidateAndExecuteHappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B", "C"})

	output := filepath.Join(tmpDir, "cropped.pdf")
	err := ValidateCropRequest(input, output, "", CropPresetSmall, nil)
	if err != nil {
		t.Fatalf("expected validate success, got %v", err)
	}

	if cropErr := Crop(context.Background(), input, output, "", CropPresetSmall, nil); cropErr != nil {
		t.Fatalf("expected crop success, got %v", cropErr)
	}

	if _, statErr := os.Stat(output); statErr != nil {
		t.Fatalf("expected output file: %v", statErr)
	}

	if validateErr := apiValidatePDF(output); validateErr != nil {
		t.Fatalf("expected valid output pdf, got %v", validateErr)
	}
}

func TestCropRejectsInvalidInput(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.txt")
	if err := os.WriteFile(input, []byte("not pdf"), 0o644); err != nil {
		t.Fatalf("write input fixture: %v", err)
	}

	output := filepath.Join(tmpDir, "cropped.pdf")
	err := ValidateCropRequest(input, output, "", CropPresetSmall, nil)
	assertCropCode(t, err, ErrorCodeValidation)
}

func TestCropRejectsExistingOutput(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	output := filepath.Join(tmpDir, "cropped.pdf")
	if err := os.WriteFile(output, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write output fixture: %v", err)
	}

	validationErr := ValidateCropRequest(input, output, "", CropPresetSmall, nil)
	assertCropCode(t, validationErr, ErrorCodeCropOutputExists)

	execErr := Crop(context.Background(), input, output, "", CropPresetSmall, nil)
	assertCropCode(t, execErr, ErrorCodeCropOutputExists)
}

func TestCropRejectsInvalidPageSelection(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B", "C"})

	output := filepath.Join(tmpDir, "cropped.pdf")
	err := ValidateCropRequest(input, output, "1,,3", CropPresetSmall, nil)
	assertCropCode(t, err, ErrorCodeCropPageSelectionBad)
}

func TestCropRejectsPageSelectionOutOfBounds(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B", "C"})

	output := filepath.Join(tmpDir, "cropped.pdf")
	err := ValidateCropRequest(input, output, "1-4", CropPresetSmall, nil)
	assertCropCode(t, err, ErrorCodeCropPageSelectionBounds)
}

func TestCropCustomRequiresMargins(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	output := filepath.Join(tmpDir, "cropped.pdf")
	err := ValidateCropRequest(input, output, "", CropPresetCustom, nil)
	assertCropCode(t, err, ErrorCodeCropMarginsRequired)
}

func TestCropValidateExecuteParityCustomWithoutMargins(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	output := filepath.Join(tmpDir, "cropped.pdf")
	validateErr := ValidateCropRequest(input, output, "", CropPresetCustom, nil)
	if validateErr == nil {
		t.Fatalf("expected validation error")
	}

	execErr := Crop(context.Background(), input, output, "", CropPresetCustom, nil)
	if execErr == nil {
		t.Fatalf("expected execute error")
	}

	if cropErr, ok := execErr.(*CropError); !ok || cropErr.Code != validateErr.Code {
		t.Fatalf("expected execute code %s, got %v", validateErr.Code, execErr)
	}
}

func TestCropBatchHappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.pdf")
	inputB := filepath.Join(tmpDir, "b.pdf")
	writeMultiPagePDF(t, inputA, []string{"A", "B"})
	writeMultiPagePDF(t, inputB, []string{"X", "Y", "Z"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	if validateErr := ValidateCropBatchRequest([]string{inputA, inputB}, outputDir, "", CropPresetSmall, nil); validateErr != nil {
		t.Fatalf("expected validate success, got %v", validateErr)
	}

	results, err := CropBatch(context.Background(), []string{inputA, inputB}, outputDir, "", CropPresetSmall, nil)
	if err != nil {
		t.Fatalf("expected batch crop success, got %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	wantA := filepath.Join(outputDir, "a_cropped.pdf")
	wantB := filepath.Join(outputDir, "b_cropped.pdf")
	if results[0].InputPath != inputA || results[0].OutputPath != wantA {
		t.Fatalf("unexpected result[0]: %+v", results[0])
	}
	if results[1].InputPath != inputB || results[1].OutputPath != wantB {
		t.Fatalf("unexpected result[1]: %+v", results[1])
	}

	for _, out := range []string{wantA, wantB} {
		if _, statErr := os.Stat(out); statErr != nil {
			t.Fatalf("expected output file %s: %v", out, statErr)
		}
	}
}

func TestCropBatchValidateAndExecuteParityPreexistingOutput(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	preexisting := filepath.Join(outputDir, "input_cropped.pdf")
	if err := os.WriteFile(preexisting, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write preexisting output: %v", err)
	}

	validateErr := ValidateCropBatchRequest([]string{input}, outputDir, "", CropPresetSmall, nil)
	assertCropCode(t, validateErr, ErrorCodeCropOutputExists)

	_, execErr := CropBatch(context.Background(), []string{input}, outputDir, "", CropPresetSmall, nil)
	assertCropCode(t, execErr, ErrorCodeCropOutputExists)
}

func TestCropBatchValidateAndExecuteParityCollision(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "A.pdf")
	inputB := filepath.Join(tmpDir, "a.PDF")
	writeMultiPagePDF(t, inputA, []string{"A", "B"})
	writeMultiPagePDF(t, inputB, []string{"X", "Y"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	validateErr := ValidateCropBatchRequest([]string{inputA, inputB}, outputDir, "", CropPresetSmall, nil)
	assertCropCode(t, validateErr, ErrorCodeCropBatchOutputCollision)

	_, execErr := CropBatch(context.Background(), []string{inputA, inputB}, outputDir, "", CropPresetSmall, nil)
	assertCropCode(t, execErr, ErrorCodeCropBatchOutputCollision)
}

func assertCropCode(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected crop error %s, got nil", expected)
	}

	cropErr, ok := err.(*CropError)
	if !ok {
		t.Fatalf("expected CropError, got %T", err)
	}

	if cropErr.Code != expected {
		t.Fatalf("expected code %s, got %s", expected, cropErr.Code)
	}
}
