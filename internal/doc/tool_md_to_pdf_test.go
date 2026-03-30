package doc

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

func TestMDToPDFToolValidateOptions(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "input.md")
	if err := os.WriteFile(mdPath, []byte("# Hello\n\ncontent"), 0o644); err != nil {
		t.Fatalf("write md fixture: %v", err)
	}

	tool := NewMDToPDFTool()

	tests := []struct {
		name       string
		req        models.JobRequestV1
		wantDetail string
	}{
		{
			name: "rejects non single",
			req: models.JobRequestV1{
				ToolID:     ToolIDDocMDToPDFV1,
				Mode:       "batch",
				InputPaths: []string{mdPath},
			},
			wantDetail: "DOC_MD_TO_PDF_MODE_INVALID",
		},
		{
			name: "rejects bad color format",
			req: models.JobRequestV1{
				ToolID:     ToolIDDocMDToPDFV1,
				Mode:       "single",
				InputPaths: []string{mdPath},
				Options: map[string]any{
					"header": map[string]any{
						"enabled":      true,
						"text":         "x",
						"align":        "left",
						"font":         "helvetica",
						"marginTop":    1,
						"marginBottom": 1,
						"color":        "red",
					},
				},
			},
			wantDetail: "DOC_MD_TO_PDF_COLOR_INVALID",
		},
		{
			name: "rejects invalid placeholder",
			req: models.JobRequestV1{
				ToolID:     ToolIDDocMDToPDFV1,
				Mode:       "single",
				InputPaths: []string{mdPath},
				Options: map[string]any{
					"footer": map[string]any{
						"enabled":      true,
						"text":         "{unknown}",
						"align":        "center",
						"font":         "times",
						"marginTop":    1,
						"marginBottom": 1,
						"color":        "#111111",
					},
				},
			},
			wantDetail: "DOC_MD_TO_PDF_PLACEHOLDER_INVALID",
		},
		{
			name: "accepts valid header footer",
			req: models.JobRequestV1{
				ToolID:     ToolIDDocMDToPDFV1,
				Mode:       "single",
				InputPaths: []string{mdPath},
				OutputDir:  tmpDir,
				Options: map[string]any{
					"header": map[string]any{
						"enabled":      true,
						"text":         "{fileName} {date}",
						"align":        "left",
						"font":         "helvetica",
						"marginTop":    0,
						"marginBottom": 2,
						"color":        "#112233",
					},
					"footer": map[string]any{
						"enabled":      true,
						"text":         "{page}/{totalPages}",
						"align":        "right",
						"font":         "courier",
						"marginTop":    1,
						"marginBottom": 1,
						"color":        "#000000",
					},
				},
			},
			wantDetail: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(context.Background(), tt.req)
			if tt.wantDetail == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %+v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error with detail %s", tt.wantDetail)
			}
			if err.DetailCode != tt.wantDetail {
				t.Fatalf("expected detail %s, got %+v", tt.wantDetail, err)
			}
		})
	}
}

func TestResolveOutputPathUsesNonDestructiveSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	input := filepath.Join(tmpDir, "sample.md")
	if err := os.WriteFile(input, []byte("# A"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	first, err := resolveOutputPath(input, tmpDir, "")
	if err != nil {
		t.Fatalf("resolve first: %+v", err)
	}
	if filepath.Base(first) != "sample_md2pdf.pdf" {
		t.Fatalf("unexpected first output: %s", first)
	}

	if writeErr := os.WriteFile(first, []byte("dummy"), 0o644); writeErr != nil {
		t.Fatalf("write first collision: %v", writeErr)
	}

	second, err := resolveOutputPath(input, tmpDir, "")
	if err != nil {
		t.Fatalf("resolve second: %+v", err)
	}
	if filepath.Base(second) != "sample_md2pdf-2.pdf" {
		t.Fatalf("unexpected second output: %s", second)
	}
}

func TestDocMDToPDFToolAppearsInRegistryCatalog(t *testing.T) {
	r := registry.NewRegistry()
	if err := r.RegisterToolV2(NewMDToPDFTool()); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDDocMDToPDFV1 {
			found = true
			if !entry.Manifest.SupportsSingle || entry.Manifest.SupportsBatch {
				t.Fatalf("unexpected manifest: %+v", entry.Manifest)
			}
		}
	}

	if !found {
		t.Fatalf("expected %s in catalog", ToolIDDocMDToPDFV1)
	}
}

func TestParseAndPrepareMapsRenderOptionsIntoEngineConfig(t *testing.T) {
	tmpDir := t.TempDir()
	mdPath := filepath.Join(tmpDir, "input.md")
	if err := os.WriteFile(mdPath, []byte("# Hello\n\ncontent"), 0o644); err != nil {
		t.Fatalf("write md fixture: %v", err)
	}

	prepared, jobErr := parseAndPrepare(models.JobRequestV1{
		ToolID:     ToolIDDocMDToPDFV1,
		Mode:       "single",
		InputPaths: []string{mdPath},
		OutputDir:  tmpDir,
		Options: map[string]any{
			"header": map[string]any{
				"enabled":      true,
				"text":         "{fileName}",
				"align":        "center",
				"font":         "times",
				"marginTop":    "4",
				"marginBottom": 3,
				"color":        "#112233",
			},
			"footer": map[string]any{
				"enabled":      true,
				"text":         "{page}/{totalPages}",
				"align":        "right",
				"font":         "courier",
				"marginTop":    2,
				"marginBottom": "5",
				"color":        "#AABBCC",
			},
		},
	})

	if jobErr != nil {
		t.Fatalf("expected nil error, got %+v", jobErr)
	}

	if !prepared.renderConfig.Header.Enabled || !prepared.renderConfig.Footer.Enabled {
		t.Fatalf("expected enabled header and footer in render config")
	}

	if prepared.renderConfig.Header.Align != "center" || prepared.renderConfig.Footer.Align != "right" {
		t.Fatalf("unexpected align mapping: header=%s footer=%s", prepared.renderConfig.Header.Align, prepared.renderConfig.Footer.Align)
	}

	if prepared.renderConfig.Header.Font != "times" || prepared.renderConfig.Footer.Font != "courier" {
		t.Fatalf("unexpected font mapping: header=%s footer=%s", prepared.renderConfig.Header.Font, prepared.renderConfig.Footer.Font)
	}

	if prepared.renderConfig.Header.MarginTop != 4 || prepared.renderConfig.Header.MarginBottom != 3 {
		t.Fatalf("unexpected header margins: top=%v bottom=%v", prepared.renderConfig.Header.MarginTop, prepared.renderConfig.Header.MarginBottom)
	}

	if prepared.renderConfig.Footer.MarginTop != 2 || prepared.renderConfig.Footer.MarginBottom != 5 {
		t.Fatalf("unexpected footer margins: top=%v bottom=%v", prepared.renderConfig.Footer.MarginTop, prepared.renderConfig.Footer.MarginBottom)
	}

	if prepared.renderConfig.Header.Color != "#112233" || prepared.renderConfig.Footer.Color != "#AABBCC" {
		t.Fatalf("unexpected color mapping: header=%s footer=%s", prepared.renderConfig.Header.Color, prepared.renderConfig.Footer.Color)
	}
}
