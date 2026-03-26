package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/registry"
)

func TestSplitToolValidateMinimumRules(t *testing.T) {
	tool := NewSplitTool()
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		req      models.JobRequestV1
		wantCode string
		wantMsg  string
	}{
		{
			name: "requires supported mode",
			req: models.JobRequestV1{
				Mode:       "weird",
				InputPaths: []string{"/tmp/input.pdf"},
				Options:    map[string]any{"outputDir": tmpDir, "strategy": engine.SplitStrategyEveryPage},
			},
			wantCode: engine.ErrorCodeValidation,
			wantMsg:  "mode must be single or batch",
		},
		{
			name: "requires exactly one input",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"a.pdf", "b.pdf"},
				Options:    map[string]any{"outputDir": tmpDir, "strategy": engine.SplitStrategyEveryPage},
			},
			wantCode: engine.ErrorCodeValidation,
			wantMsg:  "exactly 1 input PDF is required",
		},
		{
			name: "requires options outputDir",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"a.pdf"},
				Options:    map[string]any{"strategy": engine.SplitStrategyEveryPage},
			},
			wantCode: engine.ErrorCodeValidation,
			wantMsg:  "options.outputDir is required",
		},
		{
			name: "requires ranges when strategy=ranges",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"a.pdf"},
				Options:    map[string]any{"outputDir": tmpDir, "strategy": "ranges"},
			},
			wantCode: engine.ErrorCodeSplitRangesRequired,
			wantMsg:  "options.ranges is required for strategy=ranges",
		},
		{
			name: "rejects unsupported strategy",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"a.pdf"},
				Options:    map[string]any{"outputDir": tmpDir, "strategy": "weird"},
			},
			wantCode: engine.ErrorCodeUnsupportedStrategy,
			wantMsg:  "unsupported split strategy: weird",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(context.Background(), tt.req)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s", tt.wantCode, err.Code)
			}
			if err.Message != tt.wantMsg {
				t.Fatalf("expected message %q, got %q", tt.wantMsg, err.Message)
			}
		})
	}
}

func TestSplitToolExecuteSingleSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two", "three"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	tool := NewSplitTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "single",
		InputPaths: []string{input},
		OutputDir:  outputDir,
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyEveryPage,
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got error: %+v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected success item, got %+v", item)
	}
	if !strings.Contains(item.Message, "generated 3 files") {
		t.Fatalf("unexpected success message: %s", item.Message)
	}
	if item.OutputPath != outputDir {
		t.Fatalf("expected legacy outputPath %q, got %q", outputDir, item.OutputPath)
	}
	if len(item.Outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(item.Outputs))
	}
	if item.OutputCount != len(item.Outputs) {
		t.Fatalf("expected outputCount=%d, got %d", len(item.Outputs), item.OutputCount)
	}
	for _, out := range item.Outputs {
		if !strings.HasPrefix(out, outputDir+string(filepath.Separator)) {
			t.Fatalf("expected output under %q, got %q", outputDir, out)
		}
		if !strings.HasSuffix(strings.ToLower(out), ".pdf") {
			t.Fatalf("expected .pdf output, got %q", out)
		}
	}
}

func TestSplitToolExecuteSingleRangesSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two", "three", "four", "five"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	tool := NewSplitTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "single",
		InputPaths: []string{input},
		OutputDir:  outputDir,
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyRanges,
			"ranges":    "1-2,4,5",
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got error: %+v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected success item, got %+v", item)
	}
	if !strings.Contains(item.Message, "generated 3 files") {
		t.Fatalf("unexpected success message: %s", item.Message)
	}
	if item.OutputPath != outputDir {
		t.Fatalf("expected legacy outputPath %q, got %q", outputDir, item.OutputPath)
	}
	if len(item.Outputs) != 3 {
		t.Fatalf("expected 3 outputs, got %d", len(item.Outputs))
	}
	if item.OutputCount != len(item.Outputs) {
		t.Fatalf("expected outputCount=%d, got %d", len(item.Outputs), item.OutputCount)
	}
	for _, out := range item.Outputs {
		if !strings.HasPrefix(out, outputDir+string(filepath.Separator)) {
			t.Fatalf("expected output under %q, got %q", outputDir, out)
		}
	}

	wantNames := []string{
		"input_range_001_p1-2.pdf",
		"input_range_002_p4.pdf",
		"input_range_003_p5.pdf",
	}
	for i, out := range item.Outputs {
		if filepath.Base(out) != wantNames[i] {
			t.Fatalf("expected output[%d]=%q, got %q", i, wantNames[i], filepath.Base(out))
		}
	}
}

func TestSplitToolValidateRangesOutOfBounds(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two", "three"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	tool := NewSplitTool()
	err := tool.Validate(context.Background(), models.JobRequestV1{
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyRanges,
			"ranges":    "1-4",
		},
	})

	if err == nil {
		t.Fatalf("expected validation error")
	}
	if err.Code != engine.ErrorCodeSplitRangeOutBounds {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeSplitRangeOutBounds, err.Code)
	}
}

func TestSplitToolExecuteSingleCorruptInputMappedError(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "broken.pdf")
	if err := os.WriteFile(input, []byte("broken"), 0o644); err != nil {
		t.Fatalf("write corrupt input: %v", err)
	}

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	tool := NewSplitTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyEveryPage,
		},
	})

	if jobErr == nil {
		t.Fatalf("expected job error")
	}
	if jobErr.Code != engine.ErrorCodeInvalidInputPDF {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeInvalidInputPDF, jobErr.Code)
	}
	if item.Error == nil || item.Error.Code != engine.ErrorCodeInvalidInputPDF {
		t.Fatalf("expected item error %s, got %+v", engine.ErrorCodeInvalidInputPDF, item.Error)
	}
}

func TestSplitToolValidateAndExecuteParityForOutputAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	preexisting := filepath.Join(outputDir, "input_page_001.pdf")
	if err := os.WriteFile(preexisting, []byte("already exists"), 0o644); err != nil {
		t.Fatalf("write preexisting output: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyEveryPage,
		},
	}

	tool := NewSplitTool()
	validateErr := tool.Validate(context.Background(), req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}
	if validateErr.Code != engine.ErrorCodeSplitOutputExists {
		t.Fatalf("expected validate code %s, got %s", engine.ErrorCodeSplitOutputExists, validateErr.Code)
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

func TestPDFSplitToolAppearsInRegistryCatalog(t *testing.T) {
	r := registry.NewRegistry()
	if err := r.RegisterToolV2(NewSplitTool()); err != nil {
		t.Fatalf("register split tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDPDFSplitV1 {
			found = true
			if !entry.Manifest.SupportsSingle || !entry.Manifest.SupportsBatch {
				t.Fatalf("unexpected manifest capabilities: %+v", entry.Manifest)
			}
		}
	}

	if !found {
		t.Fatalf("expected tool %s in catalog", ToolIDPDFSplitV1)
	}
}

func TestSplitToolValidateBatchRequiresAtLeastOneInput(t *testing.T) {
	tool := NewSplitTool()
	err := tool.Validate(context.Background(), models.JobRequestV1{
		Mode:       "batch",
		InputPaths: []string{},
		Options: map[string]any{
			"outputDir": t.TempDir(),
			"strategy":  engine.SplitStrategyEveryPage,
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

func TestSplitToolExecuteBatchSuccessEveryPage(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "input-a.pdf")
	inputB := filepath.Join(tmpDir, "input-b.pdf")
	writeMultiPagePDFForSplitTool(t, inputA, []string{"one", "two"})
	writeMultiPagePDFForSplitTool(t, inputB, []string{"alpha", "beta", "gamma"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	tool := NewSplitTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "batch",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputDir":   outputDir,
			"strategy":    engine.SplitStrategyEveryPage,
			"perInputDir": true,
		},
	}, nil)

	if jobErr != nil {
		t.Fatalf("expected success, got error: %+v", jobErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0].InputPath != inputA || items[1].InputPath != inputB {
		t.Fatalf("expected stable order by request, got %+v", items)
	}

	if filepath.Base(items[0].OutputPath) != "input-a" {
		t.Fatalf("expected per-input dir for item0, got %s", items[0].OutputPath)
	}
	if filepath.Base(items[1].OutputPath) != "input-b" {
		t.Fatalf("expected per-input dir for item1, got %s", items[1].OutputPath)
	}

	if items[0].OutputCount != 2 || len(items[0].Outputs) != 2 {
		t.Fatalf("expected item0 output count 2, got %+v", items[0])
	}
	if items[1].OutputCount != 3 || len(items[1].Outputs) != 3 {
		t.Fatalf("expected item1 output count 3, got %+v", items[1])
	}
}

func TestSplitToolExecuteBatchSuccessRanges(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two", "three", "four"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	tool := NewSplitTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "batch",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyRanges,
			"ranges":    "1-2,4",
		},
	}, nil)

	if jobErr != nil {
		t.Fatalf("expected success, got error: %+v", jobErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].OutputCount != 2 {
		t.Fatalf("expected outputCount=2, got %+v", items[0])
	}
}

func TestSplitToolValidateAndExecuteBatchParityPreexistingOutput(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "input.pdf")
	writeMultiPagePDFForSplitTool(t, input, []string{"one", "two"})

	outputDir := filepath.Join(tmpDir, "output")
	if err := os.MkdirAll(filepath.Join(outputDir, "input"), 0o755); err != nil {
		t.Fatalf("mkdir per-input dir: %v", err)
	}

	preexisting := filepath.Join(outputDir, "input", "input_page_001.pdf")
	if err := os.WriteFile(preexisting, []byte("already exists"), 0o644); err != nil {
		t.Fatalf("write preexisting output: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "batch",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyEveryPage,
		},
	}

	tool := NewSplitTool()
	validateErr := tool.Validate(context.Background(), req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}

	items, executeErr := tool.ExecuteBatch(context.Background(), req, nil)
	if executeErr == nil {
		t.Fatalf("expected execute error")
	}
	if executeErr.Code != validateErr.Code {
		t.Fatalf("expected parity code %s, got %s", validateErr.Code, executeErr.Code)
	}
	if len(items) != 0 {
		t.Fatalf("expected no partial items on plan validation error, got %+v", items)
	}
}

func TestSplitToolValidateAndExecuteBatchParityCollision(t *testing.T) {
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
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "batch",
		InputPaths: []string{inputA, inputB},
		Options: map[string]any{
			"outputDir": outputDir,
			"strategy":  engine.SplitStrategyEveryPage,
		},
	}

	tool := NewSplitTool()
	validateErr := tool.Validate(context.Background(), req)
	if validateErr == nil {
		t.Fatalf("expected validate error")
	}
	if validateErr.Code != engine.ErrorCodeSplitBatchInputDirConflict {
		t.Fatalf("expected code %s, got %s", engine.ErrorCodeSplitBatchInputDirConflict, validateErr.Code)
	}

	_, executeErr := tool.ExecuteBatch(context.Background(), req, nil)
	if executeErr == nil {
		t.Fatalf("expected execute error")
	}
	if executeErr.Code != validateErr.Code {
		t.Fatalf("expected parity code %s, got %s", validateErr.Code, executeErr.Code)
	}
}

func TestMapSplitErrorUsesConsistentFallback(t *testing.T) {
	plain := errors.New("plain split error")
	mapped := mapSplitError(plain)
	if mapped.Code != "EXECUTION_ERROR" {
		t.Fatalf("expected EXECUTION_ERROR, got %s", mapped.Code)
	}
	if mapped.Message != plain.Error() {
		t.Fatalf("expected message %q, got %q", plain.Error(), mapped.Message)
	}
}

func writeMultiPagePDFForSplitTool(t *testing.T, path string, labels []string) {
	t.Helper()

	if len(labels) == 0 {
		labels = []string{"Page"}
	}

	builder := strings.Builder{}
	builder.WriteString("%PDF-1.4\n")

	objectCount := 3 + (2 * len(labels))
	offsets := make([]int, objectCount+1)

	writeObj := func(objID int, body string) {
		offsets[objID] = builder.Len()
		_, _ = builder.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", objID, body))
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

	xrefOffset := builder.Len()
	_, _ = builder.WriteString(fmt.Sprintf("xref\n0 %d\n", objectCount+1))
	_, _ = builder.WriteString("0000000000 65535 f \n")
	for i := 1; i <= objectCount; i++ {
		_, _ = builder.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	_, _ = builder.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", objectCount+1, xrefOffset))

	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
