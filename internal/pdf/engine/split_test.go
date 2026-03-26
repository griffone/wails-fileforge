package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestSplitEveryPageSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"Page 1", "Page 2", "Page 3"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	outputs, err := Split(context.Background(), input, outputDir, SplitStrategyEveryPage, "")
	if err != nil {
		t.Fatalf("expected split success, got error: %v", err)
	}

	if len(outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d (%v)", len(outputs), outputs)
	}

	for _, out := range outputs {
		if validateErr := apiValidatePDF(out); validateErr != nil {
			t.Fatalf("expected valid split output %s, got: %v", out, validateErr)
		}
	}

	wantNames := []string{
		"input_page_001.pdf",
		"input_page_002.pdf",
		"input_page_003.pdf",
	}
	for i, out := range outputs {
		if got := filepath.Base(out); got != wantNames[i] {
			t.Fatalf("expected deterministic output[%d]=%s, got %s", i, wantNames[i], got)
		}
	}
}

func TestSplitValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	fileAsOutput := filepath.Join(tmpDir, "not-a-dir")
	if err := os.WriteFile(fileAsOutput, []byte("x"), 0o644); err != nil {
		t.Fatalf("write output fixture: %v", err)
	}

	tests := []struct {
		name      string
		inputPath string
		outputDir string
		strategy  string
		ranges    string
		code      string
	}{
		{
			name:      "missing input path",
			inputPath: "",
			outputDir: tmpDir,
			strategy:  SplitStrategyEveryPage,
			ranges:    "",
			code:      ErrorCodeValidation,
		},
		{
			name:      "non pdf input path",
			inputPath: filepath.Join(tmpDir, "note.txt"),
			outputDir: tmpDir,
			strategy:  SplitStrategyEveryPage,
			ranges:    "",
			code:      ErrorCodeValidation,
		},
		{
			name:      "missing output dir",
			inputPath: input,
			outputDir: "",
			strategy:  SplitStrategyEveryPage,
			ranges:    "",
			code:      ErrorCodeValidation,
		},
		{
			name:      "missing output dir on filesystem",
			inputPath: input,
			outputDir: filepath.Join(tmpDir, "missing"),
			strategy:  SplitStrategyEveryPage,
			ranges:    "",
			code:      ErrorCodeOutputDirNotFound,
		},
		{
			name:      "output path points to file",
			inputPath: input,
			outputDir: fileAsOutput,
			strategy:  SplitStrategyEveryPage,
			ranges:    "",
			code:      ErrorCodeOutputDirNotDirectory,
		},
		{
			name:      "ranges strategy requires ranges",
			inputPath: input,
			outputDir: tmpDir,
			strategy:  SplitStrategyRanges,
			ranges:    "",
			code:      ErrorCodeSplitRangesRequired,
		},
		{
			name:      "unsupported strategy",
			inputPath: input,
			outputDir: tmpDir,
			strategy:  "ranges2",
			ranges:    "",
			code:      ErrorCodeUnsupportedStrategy,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := Split(context.Background(), tt.inputPath, tt.outputDir, tt.strategy, tt.ranges)
			assertSplitCode(t, err, tt.code)
		})
	}
}

func TestParseSplitRanges(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		want        []SplitPageRange
		wantErr     bool
		wantErrCode string
	}{
		{
			name: "valid mixed tokens",
			expr: "1-3, 5, 8-10",
			want: []SplitPageRange{{Start: 1, End: 3}, {Start: 5, End: 5}, {Start: 8, End: 10}},
		},
		{
			name: "valid single token",
			expr: "7",
			want: []SplitPageRange{{Start: 7, End: 7}},
		},
		{
			name:        "rejects empty expression",
			expr:        "   ",
			wantErr:     true,
			wantErrCode: ErrorCodeSplitRangesRequired,
		},
		{
			name:        "rejects empty token",
			expr:        "1-2,,3",
			wantErr:     true,
			wantErrCode: ErrorCodeSplitRangesInvalid,
		},
		{
			name:        "rejects inverted",
			expr:        "4-2",
			wantErr:     true,
			wantErrCode: ErrorCodeSplitRangesInvalid,
		},
		{
			name:        "rejects non-positive",
			expr:        "0-1",
			wantErr:     true,
			wantErrCode: ErrorCodeSplitRangesInvalid,
		},
		{
			name:        "rejects invalid token",
			expr:        "abc",
			wantErr:     true,
			wantErrCode: ErrorCodeSplitRangesInvalid,
		},
		{
			name:        "rejects duplicate page",
			expr:        "1,1",
			wantErr:     true,
			wantErrCode: ErrorCodeSplitRangesInvalid,
		},
		{
			name:        "rejects overlap",
			expr:        "1-3,3-5",
			wantErr:     true,
			wantErrCode: ErrorCodeSplitRangesInvalid,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSplitRanges(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected parse error")
				}
				if err.Code != tt.wantErrCode {
					t.Fatalf("expected code %s, got %s", tt.wantErrCode, err.Code)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected no parse error, got %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d ranges, got %d", len(tt.want), len(got))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("expected range[%d]=%+v, got %+v", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func TestSplitRangesSuccessDeterministicOutputs(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "report.pdf")
	writeMultiPagePDF(t, input, []string{"P1", "P2", "P3", "P4", "P5"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	outputs, err := Split(context.Background(), input, outputDir, SplitStrategyRanges, "1-2,4,5")
	if err != nil {
		t.Fatalf("expected split ranges success, got error: %v", err)
	}

	wantNames := []string{
		"report_range_001_p1-2.pdf",
		"report_range_002_p4.pdf",
		"report_range_003_p5.pdf",
	}
	if len(outputs) != len(wantNames) {
		t.Fatalf("expected %d outputs, got %d (%v)", len(wantNames), len(outputs), outputs)
	}

	for i, out := range outputs {
		if got := filepath.Base(out); got != wantNames[i] {
			t.Fatalf("expected deterministic output[%d]=%s, got %s", i, wantNames[i], got)
		}
		if validateErr := apiValidatePDF(out); validateErr != nil {
			t.Fatalf("expected valid split output %s, got: %v", out, validateErr)
		}
	}

	counts := []int{2, 1, 1}
	for i, out := range outputs {
		gotPages, err := api.PageCountFile(out)
		if err != nil {
			t.Fatalf("count pages for %s: %v", out, err)
		}
		if gotPages != counts[i] {
			t.Fatalf("expected %d pages for %s, got %d", counts[i], out, gotPages)
		}
	}
}

func TestSplitRangesOutOfBounds(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B", "C"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	_, err := Split(context.Background(), input, outputDir, SplitStrategyRanges, "1-4")
	assertSplitCode(t, err, ErrorCodeSplitRangeOutBounds)
}

func TestSplitRejectsOutputAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B", "C"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	existing := filepath.Join(outputDir, "input_page_001.pdf")
	if err := os.WriteFile(existing, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output fixture: %v", err)
	}

	validationErr := ValidateSplitRequest(input, outputDir, SplitStrategyEveryPage, "")
	if validationErr == nil {
		t.Fatalf("expected validation error")
	}
	if validationErr.Code != ErrorCodeSplitOutputExists {
		t.Fatalf("expected code %s, got %s", ErrorCodeSplitOutputExists, validationErr.Code)
	}

	_, err := Split(context.Background(), input, outputDir, SplitStrategyEveryPage, "")
	assertSplitCode(t, err, ErrorCodeSplitOutputExists)
}

func TestSplitBatchSuccessPerInputDirStableOrder(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "input-a.pdf")
	inputB := filepath.Join(tmpDir, "input-b.pdf")
	writeMultiPagePDF(t, inputA, []string{"A1", "A2"})
	writeMultiPagePDF(t, inputB, []string{"B1", "B2", "B3"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	results, err := SplitBatch(context.Background(), []string{inputA, inputB}, outputDir, SplitStrategyEveryPage, "", true)
	if err != nil {
		t.Fatalf("expected batch split success, got error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 batch results, got %d", len(results))
	}
	if results[0].InputPath != inputA || results[1].InputPath != inputB {
		t.Fatalf("expected stable order by request, got %+v", results)
	}
	if filepath.Base(results[0].OutputDir) != "input-a" {
		t.Fatalf("unexpected item0 output dir: %s", results[0].OutputDir)
	}
	if filepath.Base(results[1].OutputDir) != "input-b" {
		t.Fatalf("unexpected item1 output dir: %s", results[1].OutputDir)
	}
}

func TestSplitBatchRejectsInterItemCollision(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "A.pdf")
	inputB := filepath.Join(tmpDir, "a.PDF")
	writeMultiPagePDF(t, inputA, []string{"A1", "A2"})
	writeMultiPagePDF(t, inputB, []string{"B1", "B2"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	validationErr := ValidateSplitBatchRequest([]string{inputA, inputB}, outputDir, SplitStrategyEveryPage, "", true)
	if validationErr == nil {
		t.Fatalf("expected validation error")
	}
	if validationErr.Code != ErrorCodeSplitBatchInputDirConflict {
		t.Fatalf("expected %s, got %s", ErrorCodeSplitBatchInputDirConflict, validationErr.Code)
	}

	_, err := SplitBatch(context.Background(), []string{inputA, inputB}, outputDir, SplitStrategyEveryPage, "", true)
	assertSplitCode(t, err, ErrorCodeSplitBatchInputDirConflict)
}

func TestSplitBatchRejectsInterItemOutputCollisionWhenPerInputDirDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "A.pdf")
	inputB := filepath.Join(tmpDir, "a.PDF")
	writeMultiPagePDF(t, inputA, []string{"A1", "A2"})
	writeMultiPagePDF(t, inputB, []string{"B1", "B2"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	validationErr := ValidateSplitBatchRequest([]string{inputA, inputB}, outputDir, SplitStrategyEveryPage, "", false)
	if validationErr == nil {
		t.Fatalf("expected validation error")
	}
	if validationErr.Code != ErrorCodeSplitBatchOutputCollision {
		t.Fatalf("expected %s, got %s", ErrorCodeSplitBatchOutputCollision, validationErr.Code)
	}

	_, err := SplitBatch(context.Background(), []string{inputA, inputB}, outputDir, SplitStrategyEveryPage, "", false)
	assertSplitCode(t, err, ErrorCodeSplitBatchOutputCollision)
}

func TestSplitBatchRejectsPreexistingOutput(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(filepath.Join(outputDir, "input"), 0o755); err != nil {
		t.Fatalf("mkdir per-input output dir: %v", err)
	}

	preexisting := filepath.Join(outputDir, "input", "input_page_001.pdf")
	if err := os.WriteFile(preexisting, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write preexisting output fixture: %v", err)
	}

	validationErr := ValidateSplitBatchRequest([]string{input}, outputDir, SplitStrategyEveryPage, "", true)
	if validationErr == nil {
		t.Fatalf("expected validation error")
	}
	if validationErr.Code != ErrorCodeSplitOutputExists {
		t.Fatalf("expected %s, got %s", ErrorCodeSplitOutputExists, validationErr.Code)
	}

	_, err := SplitBatch(context.Background(), []string{input}, outputDir, SplitStrategyEveryPage, "", true)
	assertSplitCode(t, err, ErrorCodeSplitOutputExists)
}

func TestEnsureNoOutputCollisionsDetectsCaseInsensitiveCollision(t *testing.T) {
	planned := []splitPlannedOutput{
		{Selection: "1", Output: "/tmp/out/report_page_001.pdf"},
		{Selection: "2", Output: "/tmp/out/REPORT_PAGE_001.pdf"},
	}

	err := ensureNoOutputCollisions(planned)
	if err == nil {
		t.Fatalf("expected collision error")
	}
	if err.Code != ErrorCodeSplitOutputCollision {
		t.Fatalf("expected code %s, got %s", ErrorCodeSplitOutputCollision, err.Code)
	}
}

func TestSplitRejectsCorruptPDF(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "corrupt.pdf")
	if err := os.WriteFile(input, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	_, err := Split(context.Background(), input, outputDir, SplitStrategyEveryPage, "")
	assertSplitCode(t, err, ErrorCodeInvalidInputPDF)
}

func TestSplitCanceledContext(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Split(ctx, input, outputDir, SplitStrategyEveryPage, "")
	assertSplitCode(t, err, "CANCELED")
}

func TestSplitOutputDirectoryNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based write permission test is unreliable on windows")
	}

	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDF(t, input, []string{"A", "B"})

	outputDir := filepath.Join(tmpDir, "locked")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.Chmod(outputDir, 0o555); err != nil {
		t.Fatalf("chmod output dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(outputDir, 0o755) })

	_, err := Split(context.Background(), input, outputDir, SplitStrategyEveryPage, "")
	if err == nil {
		t.Skip("filesystem allows writes despite chmod; skipping")
	}
	assertSplitCode(t, err, ErrorCodeOutputDirNotWritable)
}

func assertSplitCode(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected split error %s, got nil", expected)
	}

	var splitErr *SplitError
	if !errors.As(err, &splitErr) {
		t.Fatalf("expected SplitError, got %T", err)
	}

	if splitErr.Code != expected {
		t.Fatalf("expected code %s, got %s", expected, splitErr.Code)
	}
}

func apiValidatePDF(path string) error {
	return api.ValidateFile(path, nil)
}

func writeMultiPagePDF(t *testing.T, path string, labels []string) {
	t.Helper()

	if len(labels) == 0 {
		labels = []string{"Page"}
	}

	buf := strings.Builder{}
	buf.WriteString("%PDF-1.4\n")

	objectCount := 3 + (2 * len(labels))
	offsets := make([]int, objectCount+1)

	writeObj := func(objID int, body string) {
		offsets[objID] = buf.Len()
		_, _ = fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", objID, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")

	pageRefs := make([]string, 0, len(labels))
	for i := range labels {
		pageObj := 3 + (i * 2)
		pageRefs = append(pageRefs, fmt.Sprintf("%d 0 R", pageObj))
	}
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(pageRefs, " "), len(labels)))

	for i, label := range labels {
		pageObj := 3 + (i * 2)
		contentObj := pageObj + 1
		stream := fmt.Sprintf("BT /F1 18 Tf 40 100 Td (%s) Tj ET", label)
		writeObj(pageObj, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", contentObj, objectCount))
		writeObj(contentObj, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}

	writeObj(objectCount, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xrefOffset := buf.Len()
	_, _ = fmt.Fprintf(&buf, "xref\n0 %d\n", objectCount+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= objectCount; i++ {
		_, _ = fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	_, _ = fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", objectCount+1, xrefOffset)

	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("failed writing multi-page pdf: %v", err)
	}
}
