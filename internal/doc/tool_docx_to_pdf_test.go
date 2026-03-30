package doc

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

type fakeDOCXProbe struct {
	err error
}

func (p fakeDOCXProbe) Check(_ context.Context) error {
	return p.err
}

type fakeDOCXToolRunner struct {
	runs []fakeDOCXToolRun
	idx  int
}

type fakeDOCXToolRun struct {
	name   string
	result engine.CommandResult
	err    error
	hook   func(args ...string)
}

func (f *fakeDOCXToolRunner) Run(_ context.Context, name string, args ...string) (engine.CommandResult, error) {
	if f.idx >= len(f.runs) {
		return engine.CommandResult{}, nil
	}
	r := f.runs[f.idx]
	f.idx++
	if r.hook != nil {
		r.hook(args...)
	}
	if r.name != "" && r.name != name {
		return engine.CommandResult{}, errors.New("unexpected command name")
	}
	return r.result, r.err
}

func TestDOCXToPDFToolValidateModeAndExtensions(t *testing.T) {
	tmpDir := t.TempDir()
	docxPath := createToolDOCXFixture(t, tmpDir, false)
	textPath := filepath.Join(tmpDir, "input.txt")
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write txt fixture: %v", err)
	}

	tool := NewDOCXToPDFToolWithDeps(fakeDOCXProbe{}, &fakeDOCXToolRunner{})

	tests := []struct {
		name       string
		req        models.JobRequestV1
		wantDetail string
	}{
		{
			name: "rejects invalid mode",
			req: models.JobRequestV1{
				ToolID:     ToolIDDocDOCXToPDFV1,
				Mode:       "invalid",
				InputPaths: []string{docxPath},
			},
			wantDetail: "DOC_DOCX_TO_PDF_MODE_INVALID",
		},
		{
			name: "rejects non docx input",
			req: models.JobRequestV1{
				ToolID:     ToolIDDocDOCXToPDFV1,
				Mode:       "single",
				InputPaths: []string{textPath},
			},
			wantDetail: "DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED",
		},
		{
			name: "accepts valid single",
			req: models.JobRequestV1{
				ToolID:     ToolIDDocDOCXToPDFV1,
				Mode:       "single",
				InputPaths: []string{docxPath},
				OutputDir:  tmpDir,
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

			if err == nil || err.DetailCode != tt.wantDetail {
				t.Fatalf("expected detail %s, got %+v", tt.wantDetail, err)
			}
		})
	}
}

func TestDOCXToPDFToolExecuteBatchReturnsPartialFailures(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := createToolDOCXFixture(t, tmpDir, false)
	inputB := createToolDOCXFixtureNamed(t, tmpDir, "b.docx", false)

	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir out dir: %v", err)
	}

	runner := &fakeDOCXToolRunner{runs: []fakeDOCXToolRun{
		{
			name: "libreoffice",
			hook: func(args ...string) {
				converted := filepath.Join(extractOutDirArg(args), "input.pdf")
				_ = os.WriteFile(converted, []byte("%PDF-1.4\n"), 0o644)
			},
		},
		{name: "libreoffice", err: errors.New("libreoffice boom"), result: engine.CommandResult{Stderr: "error"}},
		{name: "pandoc", err: errors.New("pandoc boom"), result: engine.CommandResult{Stderr: "error"}},
	}}

	tool := NewDOCXToPDFToolWithDeps(fakeDOCXProbe{}, runner)
	items, err := tool.ExecuteBatch(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDDocDOCXToPDFV1,
		Mode:       "batch",
		InputPaths: []string{inputA, inputB},
		OutputDir:  outDir,
	}, nil)

	if err == nil {
		t.Fatalf("expected firstErr for mixed batch")
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].Success || items[1].Success {
		t.Fatalf("expected [success, failure], got %+v", items)
	}
}

func TestDOCXToPDFToolManifestAndRegistry(t *testing.T) {
	tool := NewDOCXToPDFTool()
	manifest := tool.Manifest()
	if manifest.ToolID != ToolIDDocDOCXToPDFV1 {
		t.Fatalf("unexpected tool id: %s", manifest.ToolID)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatalf("unexpected single/batch support: %+v", manifest)
	}

	r := registry.NewRegistry()
	if err := r.RegisterToolV2(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDDocDOCXToPDFV1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected %s in registry catalog", ToolIDDocDOCXToPDFV1)
	}
}

func TestDOCXOutputNamingNonDestructive(t *testing.T) {
	tmpDir := t.TempDir()
	input := createToolDOCXFixture(t, tmpDir, false)
	outDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir out dir: %v", err)
	}

	first := nextAvailableDOCXOutput(outDir, input, nil)
	if filepath.Base(first) != "input_docx2pdf.pdf" {
		t.Fatalf("unexpected first output: %s", first)
	}
	if err := os.WriteFile(first, []byte("occupied"), 0o644); err != nil {
		t.Fatalf("occupy first: %v", err)
	}

	second := nextAvailableDOCXOutput(outDir, input, nil)
	if filepath.Base(second) != "input_docx2pdf-2.pdf" {
		t.Fatalf("unexpected second output: %s", second)
	}
}

func createToolDOCXFixture(t *testing.T, dir string, protected bool) string {
	t.Helper()
	return createToolDOCXFixtureNamed(t, dir, "input.docx", protected)
}

func createToolDOCXFixtureNamed(t *testing.T, dir, name string, protected bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}

	zw := zip.NewWriter(f)
	_, _ = zw.Create("[Content_Types].xml")
	_, _ = zw.Create("word/document.xml")

	settingsWriter, err := zw.Create("word/settings.xml")
	if err != nil {
		t.Fatalf("create settings: %v", err)
	}

	settings := `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"></w:settings>`
	if protected {
		settings = `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:documentProtection w:edit="readOnly"/></w:settings>`
	}
	if _, err := settingsWriter.Write([]byte(settings)); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}

	return path
}

func extractOutDirArg(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--outdir" {
			return args[i+1]
		}
	}
	return ""
}
