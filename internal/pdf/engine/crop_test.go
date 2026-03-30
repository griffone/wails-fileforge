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
	if !results[0].Success || !results[1].Success {
		t.Fatalf("expected successful batch item results, got %+v", results)
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

func TestCropBatchOutputAutoincrementWhenPreexisting(t *testing.T) {
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

	if validateErr := ValidateCropBatchRequest([]string{input}, outputDir, "", CropPresetSmall, nil); validateErr != nil {
		t.Fatalf("expected validate success with preexisting output, got %v", validateErr)
	}

	results, execErr := CropBatch(context.Background(), []string{input}, outputDir, "", CropPresetSmall, nil)
	if execErr != nil {
		t.Fatalf("expected execute success, got %v", execErr)
	}
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("expected one successful result, got %+v", results)
	}

	if results[0].OutputPath != filepath.Join(outputDir, "input_cropped-2.pdf") {
		t.Fatalf("expected autoincremented output path, got %s", results[0].OutputPath)
	}
}

func TestCropBatchOutputAutoincrementForCaseInsensitiveNameCollision(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "A.pdf")
	inputB := filepath.Join(tmpDir, "a.PDF")
	writeMultiPagePDF(t, inputA, []string{"A", "B"})
	writeMultiPagePDF(t, inputB, []string{"X", "Y"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	if validateErr := ValidateCropBatchRequest([]string{inputA, inputB}, outputDir, "", CropPresetSmall, nil); validateErr != nil {
		t.Fatalf("expected validate success on case-insensitive name collision, got %v", validateErr)
	}

	results, execErr := CropBatch(context.Background(), []string{inputA, inputB}, outputDir, "", CropPresetSmall, nil)
	if execErr != nil {
		t.Fatalf("expected execute success, got %v", execErr)
	}
	if len(results) != 2 {
		t.Fatalf("expected two results, got %d", len(results))
	}
	if !results[0].Success || !results[1].Success {
		t.Fatalf("expected all item success, got %+v", results)
	}

	if results[0].OutputPath != filepath.Join(outputDir, "A_cropped.pdf") {
		t.Fatalf("unexpected output path for first item: %s", results[0].OutputPath)
	}
	if results[1].OutputPath != filepath.Join(outputDir, "a_cropped-2.pdf") {
		t.Fatalf("expected second item with incremented suffix, got %s", results[1].OutputPath)
	}
}

func TestCropBatchMixedPageSelectionOutOfBoundsReturnsItemError(t *testing.T) {
	tmpDir := t.TempDir()
	inputShort := filepath.Join(tmpDir, "short.pdf")
	inputLong := filepath.Join(tmpDir, "long.pdf")
	writeMultiPagePDF(t, inputShort, []string{"A", "B"})
	writeMultiPagePDF(t, inputLong, []string{"1", "2", "3", "4"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	results, execErr := CropBatch(context.Background(), []string{inputShort, inputLong}, outputDir, "3-4", CropPresetSmall, nil)
	if execErr != nil {
		t.Fatalf("expected no fatal batch error, got %v", execErr)
	}
	if len(results) != 2 {
		t.Fatalf("expected two item results, got %d", len(results))
	}

	if results[0].Success {
		t.Fatalf("expected first item failure, got %+v", results[0])
	}
	if results[0].Error == nil || results[0].Error.Code != ErrorCodeCropPageSelectionBounds {
		t.Fatalf("expected bounds error on first item, got %+v", results[0].Error)
	}

	if !results[1].Success {
		t.Fatalf("expected second item success, got %+v", results[1])
	}
}

func TestCropBatchInvalidSyntaxGlobalFailFastDoesNotProcessItems(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.pdf")
	inputB := filepath.Join(tmpDir, "b.pdf")
	writeMultiPagePDF(t, inputA, []string{"A", "B"})
	writeMultiPagePDF(t, inputB, []string{"X", "Y"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	results, execErr := CropBatch(context.Background(), []string{inputA, inputB}, outputDir, "1,,3", CropPresetSmall, nil)
	if execErr == nil {
		t.Fatalf("expected fail-fast batch error for invalid global pageSelection syntax")
	}
	assertCropCode(t, execErr, ErrorCodeCropPageSelectionBad)

	if len(results) != 0 {
		t.Fatalf("expected no processed items on fail-fast validation, got %d", len(results))
	}

	if _, statErr := os.Stat(filepath.Join(outputDir, "a_cropped.pdf")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no output for first input, got statErr=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(outputDir, "b_cropped.pdf")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no output for second input, got statErr=%v", statErr)
	}
}

func TestCropPageSelectionSyntaxValidationForCrop(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		wantCode  string
		wantError bool
	}{
		{name: "empty expression is allowed", expr: "", wantError: false},
		{name: "valid mixed expression", expr: "1-3,5,8-10", wantError: false},
		{name: "empty token", expr: "1,,3", wantCode: ErrorCodeCropPageSelectionBad, wantError: true},
		{name: "start greater than end", expr: "4-2", wantCode: ErrorCodeCropPageSelectionBad, wantError: true},
		{name: "non numeric token", expr: "1,a", wantCode: ErrorCodeCropPageSelectionBad, wantError: true},
		{name: "overlap detection", expr: "1-3,3-5", wantCode: ErrorCodeCropPageSelectionBad, wantError: true},
	}

	for _, tt := range tests {
		tc := tt
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCropPageSelectionSyntax(tc.expr)
			if tc.wantError {
				if err == nil {
					t.Fatalf("expected error")
				}
				if err.Code != tc.wantCode {
					t.Fatalf("expected code %s, got %s", tc.wantCode, err.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
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
