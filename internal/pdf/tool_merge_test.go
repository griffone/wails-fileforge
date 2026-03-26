package pdf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/registry"
)

func TestMergeToolValidateMinimumRules(t *testing.T) {
	tool := NewMergeTool()
	tests := []struct {
		name     string
		req      models.JobRequestV1
		wantCode string
		want     string
	}{
		{
			name: "requires single mode",
			req: models.JobRequestV1{
				Mode:       "batch",
				InputPaths: []string{"a.pdf", "b.pdf"},
				Options:    map[string]any{"outputPath": "out.pdf"},
			},
			wantCode: "VALIDATION_ERROR",
			want:     "mode must be single",
		},
		{
			name: "requires at least 2 inputs",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"a.pdf"},
				Options:    map[string]any{"outputPath": "out.pdf"},
			},
			wantCode: "VALIDATION_ERROR",
			want:     "at least 2 input PDFs are required",
		},
		{
			name: "requires pdf extension",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"a.pdf", "b.txt"},
				Options:    map[string]any{"outputPath": "out.pdf"},
			},
			wantCode: "VALIDATION_ERROR",
			want:     "input file must be .pdf: b.txt",
		},
		{
			name: "requires outputPath",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"a.pdf", "b.pdf"},
				Options:    map[string]any{},
			},
			wantCode: "VALIDATION_ERROR",
			want:     "outputPath is required",
		},
		{
			name: "rejects duplicate input paths case insensitive",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"/tmp/A.pdf", "/tmp/a.PDF"},
				Options:    map[string]any{"outputPath": "out.pdf"},
			},
			wantCode: engine.ErrorCodeDuplicateInput,
			want:     "duplicate input path: /tmp/a.PDF",
		},
		{
			name: "rejects output colliding with any input",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{"/tmp/a.pdf", "/tmp/b.pdf"},
				Options:    map[string]any{"outputPath": " /tmp/A.PDF "},
			},
			wantCode: engine.ErrorCodeOutputCollidesInput,
			want:     "outputPath collides with input path: /tmp/a.pdf",
		},
		{
			name: "rejects windows-like duplicate input paths",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{`C:\Docs\A.pdf`, `c:/docs/a.PDF`},
				Options:    map[string]any{"outputPath": "out.pdf"},
			},
			wantCode: engine.ErrorCodeDuplicateInput,
			want:     "duplicate input path: c:/docs/a.PDF",
		},
		{
			name: "rejects windows-like output collision with input",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{`C:\Docs\A.pdf`, `C:\Docs\B.pdf`},
				Options:    map[string]any{"outputPath": ` c:/docs/a.PDF `},
			},
			wantCode: engine.ErrorCodeOutputCollidesInput,
			want:     "outputPath collides with input path: C:\\Docs\\A.pdf",
		},
		{
			name: "rejects unc style output collision with input",
			req: models.JobRequestV1{
				Mode:       "single",
				InputPaths: []string{`\\Server\Share\A.pdf`, `\\Server\Share\B.pdf`},
				Options:    map[string]any{"outputPath": `//server/share/a.PDF`},
			},
			wantCode: engine.ErrorCodeOutputCollidesInput,
			want:     `outputPath collides with input path: \\Server\Share\A.pdf`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if err.Code != tt.wantCode {
				t.Fatalf("expected %s, got %s", tt.wantCode, err.Code)
			}
			if err.Message != tt.want {
				t.Fatalf("expected message %q, got %q", tt.want, err.Message)
			}
		})
	}
}

func TestMergeToolExecuteSingleCorruptInputMappedError(t *testing.T) {
	tmpDir := t.TempDir()
	validPDF := filepath.Join(tmpDir, "valid.pdf")
	corruptPDF := filepath.Join(tmpDir, "corrupt.pdf")
	out := filepath.Join(tmpDir, "merged.pdf")

	writeMinimalPDFFile(t, validPDF)
	if err := os.WriteFile(corruptPDF, []byte("corrupt-content"), 0o644); err != nil {
		t.Fatalf("failed writing corrupt pdf fixture: %v", err)
	}

	tool := NewMergeTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDPDFMergeV1,
		Mode:       "single",
		InputPaths: []string{validPDF, corruptPDF},
		Options:    map[string]any{"outputPath": out},
	})

	if jobErr == nil {
		t.Fatal("expected execution error, got nil")
	}

	if jobErr.Code != engine.ErrorCodeInvalidInputPDF {
		t.Fatalf("expected %s, got %s", engine.ErrorCodeInvalidInputPDF, jobErr.Code)
	}

	if item.Success {
		t.Fatalf("expected failed item result")
	}
	if item.Error == nil || item.Error.Code != engine.ErrorCodeInvalidInputPDF {
		t.Fatalf("expected item error with code %s, got %+v", engine.ErrorCodeInvalidInputPDF, item.Error)
	}
}

func TestPDFMergeToolAppearsInRegistryCatalog(t *testing.T) {
	r := registry.NewRegistry()
	tool := NewMergeTool()
	if err := r.RegisterToolV2(tool); err != nil {
		t.Fatalf("register merge tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDPDFMergeV1 {
			found = true
			if !entry.Manifest.SupportsSingle || entry.Manifest.SupportsBatch {
				t.Fatalf("unexpected manifest capabilities: %+v", entry.Manifest)
			}
		}
	}

	if !found {
		t.Fatalf("expected tool %s in catalog", ToolIDPDFMergeV1)
	}
}

func TestMapMergeErrorUsesConsistentFallback(t *testing.T) {
	customErr := errors.New("plain error")
	mapped := mapMergeError(customErr)
	if mapped.Code != "EXECUTION_ERROR" {
		t.Fatalf("expected EXECUTION_ERROR, got %s", mapped.Code)
	}
	if mapped.Message != customErr.Error() {
		t.Fatalf("expected message %q, got %q", customErr.Error(), mapped.Message)
	}
}

func writeMinimalPDFFile(t *testing.T, path string) {
	t.Helper()

	content := []byte("%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >> endobj\n4 0 obj << /Length 30 >> stream\nBT /F1 18 Tf 40 100 Td (Hi) Tj ET\nendstream endobj\nxref\n0 5\n0000000000 65535 f \n0000000010 00000 n \n0000000063 00000 n \n0000000126 00000 n \n0000000208 00000 n \ntrailer << /Size 5 /Root 1 0 R >>\nstartxref\n292\n%%EOF\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("failed writing minimal pdf: %v", err)
	}
}
