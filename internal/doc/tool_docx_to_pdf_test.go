package doc

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
)

type stubDOCXRuntimeProbe struct {
	err error
}

func (p stubDOCXRuntimeProbe) Check(context.Context) error {
	return p.err
}

type stubDOCXRunner struct {
	run func(ctx context.Context, name string, args ...string) (engine.CommandResult, error)
}

func (r stubDOCXRunner) Run(ctx context.Context, name string, args ...string) (engine.CommandResult, error) {
	return r.run(ctx, name, args...)
}

// newPDFProducingDOCXRunner fails libreoffice and makes the pandoc fallback
// write a PDF with the given page count to the requested output path.
func newPDFProducingDOCXRunner(t *testing.T, pages int) stubDOCXRunner {
	t.Helper()

	return stubDOCXRunner{run: func(_ context.Context, name string, args ...string) (engine.CommandResult, error) {
		switch name {
		case "libreoffice":
			return engine.CommandResult{}, errors.New("libreoffice unavailable in tests")
		case "pandoc":
			if len(args) < 3 {
				t.Fatalf("unexpected pandoc args: %v", args)
			}
			writeDOCXPDFFixture(t, args[2], pages)
			return engine.CommandResult{}, nil
		default:
			t.Fatalf("unexpected command: %s", name)
			return engine.CommandResult{}, nil
		}
	}}
}

func writeDOCXPDFFixture(t *testing.T, path string, pages int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create pdf fixture dir: %v", err)
	}

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetFont("Helvetica", "", 12)
	for i := 1; i <= pages; i++ {
		pdf.AddPage()
		pdf.Cell(0, 10, fmt.Sprintf("Page %d", i))
	}

	if err := pdf.OutputFileAndClose(path); err != nil {
		t.Fatalf("write pdf fixture: %v", err)
	}
}

func docxPDFPageCount(t *testing.T, path string) int {
	t.Helper()

	dims, err := api.PageDimsFile(context.Background(), path)
	if err != nil {
		t.Fatalf("read page dimensions from %s: %v", path, err)
	}

	return len(dims)
}

func newTestDOCXTool() *DOCXToPDFTool {
	return NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, nil)
}

// writeDOCXFixture creates a minimal .docx package with the given settings XML.
func writeDOCXFixture(t *testing.T, path, settingsXML string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}

	archive := zip.NewWriter(file)
	entry, err := archive.Create("word/settings.xml")
	if err != nil {
		t.Fatalf("create docx entry: %v", err)
	}
	if _, err := entry.Write([]byte(settingsXML)); err != nil {
		t.Fatalf("write docx entry: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}

func writePlainDOCXFixture(t *testing.T, path string) {
	t.Helper()
	writeDOCXFixture(t, path, "<w:settings/>")
}

func docxPlanRequest(mode string, inputPaths []string, outputDir string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     ToolIDDocDOCXToPDF,
		Mode:       mode,
		InputPaths: inputPaths,
		OutputDir:  outputDir,
		Options:    options,
	}
}

func TestDOCXPlanSingleAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected success plan")
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	want := filepath.Join(dir, "report_docx2pdf.pdf")
	if output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.InputPath != input {
		t.Fatalf("expected input %q, got %q", input, output.InputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Exists {
		t.Fatal("expected planned output to not exist yet")
	}
	if output.Adjusted {
		t.Fatal("expected natural output name to be unadjusted")
	}
	if _, statErr := os.Stat(want); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}
}

func TestDOCXPlanSingleUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, outDir, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	want := filepath.Join(outDir, "report_docx2pdf.pdf")
	if plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestDOCXPlanBatchWithoutOutputDirPlansEachInputDirectory(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.docx")
	second := filepath.Join(root, "two", "second.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("batch", []string{first, second}, "", nil))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	if want := filepath.Join(root, "one", "first_docx2pdf.pdf"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(root, "two", "second_docx2pdf.pdf"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	for _, output := range plan.Outputs {
		if output.Adjusted || output.Exists {
			t.Fatalf("expected fresh outputs, got %+v", output)
		}
	}
}

func TestDOCXPlanBatchUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	first := filepath.Join(dir, "first.docx")
	second := filepath.Join(dir, "second.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("batch", []string{first, second}, outDir, nil))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(outDir, "first_docx2pdf.pdf"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(outDir, "second_docx2pdf.pdf"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
}

func TestDOCXPlanBatchSuffixesExistingAndDuplicateOutputs(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	first := filepath.Join(root, "one", "report.docx")
	second := filepath.Join(root, "two", "report.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	natural := filepath.Join(outDir, "report_docx2pdf.pdf")
	writeDocFixture(t, natural, "existing")

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("batch", []string{first, second}, outDir, nil))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	if want := filepath.Join(outDir, "report_docx2pdf-2.pdf"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(outDir, "report_docx2pdf-3.pdf"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	for _, output := range plan.Outputs {
		if !output.Adjusted {
			t.Fatalf("expected adjusted output, got %+v", output)
		}
		if len(output.Warnings) == 0 {
			t.Fatalf("expected a warning for %q", output.OutputPath)
		}
	}
	if len(plan.Warnings) != 2 {
		t.Fatalf("expected 2 plan warnings, got %v", plan.Warnings)
	}

	content, readErr := os.ReadFile(natural)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestDOCXPlanExplicitOutputSuffixesExistingFile(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)
	explicit := filepath.Join(dir, "custom.pdf")
	writeDocFixture(t, explicit, "existing")

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"outputPath": explicit}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "custom-2.pdf"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag for the explicit collision")
	}

	content, readErr := os.ReadFile(explicit)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestDOCXPlanRejectsUnsupportedInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.txt")
	writeDocFixture(t, input, "not a docx")

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", nil))
	if planErr == nil {
		t.Fatal("expected unsupported input error")
	}
	if planErr.DetailCode != "DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED" {
		t.Fatalf("expected DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED, got %q", planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "notes.txt") {
		t.Fatalf("expected message to name the file, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestDOCXPlanRejectsProtectedInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "protected.docx")
	writeDOCXFixture(t, input, "<w:settings><w:documentProtection/></w:settings>")

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", nil))
	if planErr == nil {
		t.Fatal("expected protected input error")
	}
	if planErr.DetailCode != engine.ErrorCodeDocxInputProtected {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeDocxInputProtected, planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "protected.docx") {
		t.Fatalf("expected message to name the file, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestDOCXPlanReportsMissingRuntime(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	probeErr := &engine.DOCXError{
		Code:    engine.ErrorCodeDocxRuntimePrimaryMissing,
		Message: "libreoffice binary not found in PATH",
	}
	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{err: probeErr}, nil)

	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", nil))
	if planErr == nil {
		t.Fatal("expected runtime error")
	}
	if planErr.DetailCode != engine.ErrorCodeDocxRuntimePrimaryMissing {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeDocxRuntimePrimaryMissing, planErr.DetailCode)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestDOCXValidateBatchWithoutOutputDir(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.docx")
	second := filepath.Join(root, "second.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	tool := newTestDOCXTool()
	if validateErr := tool.Validate(context.Background(), docxPlanRequest("batch", []string{first, second}, "", nil)); validateErr != nil {
		t.Fatalf("expected batch validation without outputDir to pass, got %v", validateErr)
	}
}

func TestDOCXPlanRejectsInvalidPageSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	for _, selection := range []string{"abc", "1,,3", "1-2-3"} {
		t.Run(selection, func(t *testing.T) {
			tool := newTestDOCXTool()
			plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pageSelection": selection}))
			if planErr == nil {
				t.Fatal("expected an invalid page selection error")
			}
			if planErr.DetailCode != "DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID" {
				t.Fatalf("expected DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID, got %q", planErr.DetailCode)
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
		})
	}
}

func TestDOCXPlanIncludesPageSelectionInEffectiveOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := newTestDOCXTool()
	plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pageSelection": " 1-3, 5 "}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if got := plan.EffectiveOptions["pageSelection"]; got != "1-3,5" {
		t.Fatalf("expected trimmed pageSelection in effective options, got %#v", got)
	}

	defaultPlan, defaultErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", nil))
	if defaultErr != nil {
		t.Fatalf("expected default plan to succeed, got %v", defaultErr)
	}
	if defaultPlan.EffectiveOptions != nil {
		t.Fatalf("expected no effective options without a page selection, got %#v", defaultPlan.EffectiveOptions)
	}
}

func TestDOCXPlanIncludesPDFAInEffectiveOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := newTestDOCXTool()

	pdfaPlan, pdfaErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pdfa": true}))
	if pdfaErr != nil {
		t.Fatalf("expected pdf/a plan to succeed, got %v", pdfaErr)
	}
	if got := pdfaPlan.EffectiveOptions["pdfa"]; got != true {
		t.Fatalf("expected pdfa true in effective options, got %#v", got)
	}
	if len(pdfaPlan.Warnings) != 0 {
		t.Fatalf("expected no warnings for pdf/a without a page selection, got %v", pdfaPlan.Warnings)
	}

	// pdfa false is the default and must not pollute the effective options.
	plainPlan, plainErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pdfa": false}))
	if plainErr != nil {
		t.Fatalf("expected plain plan to succeed, got %v", plainErr)
	}
	if plainPlan.EffectiveOptions != nil {
		t.Fatalf("expected no effective options for pdfa false, got %#v", plainPlan.EffectiveOptions)
	}

	combinedPlan, combinedErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pdfa": true, "pageSelection": "1-2"}))
	if combinedErr != nil {
		t.Fatalf("expected combined plan to succeed, got %v", combinedErr)
	}
	if got := combinedPlan.EffectiveOptions["pageSelection"]; got != "1-2" {
		t.Fatalf("expected pageSelection in combined effective options, got %#v", got)
	}
	if got := combinedPlan.EffectiveOptions["pdfa"]; got != true {
		t.Fatalf("expected pdfa in combined effective options, got %#v", got)
	}
	if len(combinedPlan.Warnings) != 1 || !strings.Contains(combinedPlan.Warnings[0], "PDF/A") {
		t.Fatalf("expected a PDF/A page-selection warning, got %v", combinedPlan.Warnings)
	}
}

func TestDOCXPlanRejectsInvalidPDFAOption(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	for _, value := range []any{"yes", 1, []string{"true"}} {
		t.Run(fmt.Sprintf("%v", value), func(t *testing.T) {
			tool := newTestDOCXTool()
			plan, planErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pdfa": value}))
			if planErr == nil {
				t.Fatal("expected an invalid pdfa option error")
			}
			if planErr.DetailCode != "DOC_DOCX_TO_PDF_OPTION_INVALID" {
				t.Fatalf("expected DOC_DOCX_TO_PDF_OPTION_INVALID, got %q", planErr.DetailCode)
			}
			if !strings.Contains(planErr.Message, "pdfa") {
				t.Fatalf("expected message to name the option, got %q", planErr.Message)
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
		})
	}
}

// newLibreOfficePDFDOCXRunner writes a PDF fixture for every libreoffice call and
// records the --convert-to value so tests can assert the export filter.
func newLibreOfficePDFDOCXRunner(t *testing.T, pages int, wantConvertTo string) stubDOCXRunner {
	t.Helper()

	return stubDOCXRunner{run: func(_ context.Context, name string, args ...string) (engine.CommandResult, error) {
		if name != "libreoffice" {
			t.Fatalf("expected libreoffice, got %q with args %v", name, args)
		}

		convertTo, outDir := "", ""
		for index := 0; index < len(args); index++ {
			switch args[index] {
			case "--convert-to":
				convertTo = args[index+1]
				index++
			case "--outdir":
				outDir = args[index+1]
				index++
			}
		}
		if convertTo != wantConvertTo {
			t.Fatalf("expected --convert-to %q, got %q (args: %v)", wantConvertTo, convertTo, args)
		}
		if outDir == "" {
			t.Fatalf("expected --outdir in args, got %v", args)
		}
		if len(args) == 0 {
			t.Fatal("expected the input path as the last argument")
		}

		inputPath := args[len(args)-1]
		converted := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))+".pdf")
		writeDOCXPDFFixture(t, converted, pages)

		return engine.CommandResult{}, nil
	}}
}

func TestDOCXExecuteSingleUsesPDFAExportFilter(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newLibreOfficePDFDOCXRunner(t, 3, `pdf:writer_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"1"}}`))
	item, itemErr := tool.ExecuteSingle(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pdfa": true}))
	if itemErr != nil {
		t.Fatalf("expected pdf/a execution to succeed, got %v", itemErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %q", item.Message)
	}
	if got := docxPDFPageCount(t, item.OutputPath); got != 3 {
		t.Fatalf("expected 3 pages, got %d", got)
	}
}

func TestDOCXExecuteBatchUsesPDFAExportFilter(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.docx")
	second := filepath.Join(dir, "second.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newLibreOfficePDFDOCXRunner(t, 2, `pdf:writer_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"1"}}`))
	items, batchErr := tool.ExecuteBatch(context.Background(), docxPlanRequest("batch", []string{first, second}, "", map[string]any{"pdfa": true}), nil)
	if batchErr != nil {
		t.Fatalf("expected pdf/a batch execution to succeed, got %v", batchErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 batch items, got %d", len(items))
	}
	for _, item := range items {
		if !item.Success {
			t.Fatalf("expected a successful item, got %q", item.Message)
		}
	}
}

func TestDOCXExecutePDFAFailsWhenLibreOfficeFails(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newPDFProducingDOCXRunner(t, 3))
	item, itemErr := tool.ExecuteSingle(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pdfa": true}))
	if itemErr == nil {
		t.Fatal("expected a pdf/a unsupported error")
	}
	if itemErr.DetailCode != engine.ErrorCodeDocxPDFAUnsupported {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeDocxPDFAUnsupported, itemErr.DetailCode)
	}
	if item.Success {
		t.Fatal("expected the item to report failure")
	}
	if _, statErr := os.Stat(item.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no non-archival output, got %v", statErr)
	}
}

func TestDOCXExecuteSingleAppliesPageSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newPDFProducingDOCXRunner(t, 5))
	req := docxPlanRequest("single", []string{input}, "", map[string]any{"pageSelection": "2-4"})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr != nil {
		t.Fatalf("expected execution to succeed, got %v", itemErr)
	}
	if item.OutputPath != plan.Outputs[0].OutputPath {
		t.Fatalf("execution wrote %q but the plan promised %q", item.OutputPath, plan.Outputs[0].OutputPath)
	}

	if got := docxPDFPageCount(t, item.OutputPath); got != 3 {
		t.Fatalf("expected 3 pages after applying the selection, got %d", got)
	}
}

func TestDOCXExecuteSingleFailsWhenPageSelectionExceedsDocument(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newPDFProducingDOCXRunner(t, 2))

	item, itemErr := tool.ExecuteSingle(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pageSelection": "9-12"}))
	if itemErr == nil {
		t.Fatal("expected an out-of-range page selection error")
	}
	if itemErr.DetailCode != "DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID" {
		t.Fatalf("expected DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID, got %q", itemErr.DetailCode)
	}
	if !strings.Contains(itemErr.Message, "report.docx") {
		t.Fatalf("expected message to name the input, got %q", itemErr.Message)
	}
	if item.Success {
		t.Fatal("expected the item to report failure")
	}
	if _, statErr := os.Stat(item.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no partial output, got %v", statErr)
	}
}

func TestDOCXExecuteBatchAppliesPageSelectionToEveryDocument(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.docx")
	second := filepath.Join(dir, "second.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newPDFProducingDOCXRunner(t, 4))
	items, batchErr := tool.ExecuteBatch(context.Background(), docxPlanRequest("batch", []string{first, second}, "", map[string]any{"pageSelection": "1-2"}), nil)
	if batchErr != nil {
		t.Fatalf("expected batch execution to succeed, got %v", batchErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 batch items, got %d", len(items))
	}

	for _, item := range items {
		if !item.Success {
			t.Fatalf("expected successful item, got %q", item.Message)
		}
		if got := docxPDFPageCount(t, item.OutputPath); got != 2 {
			t.Fatalf("expected 2 pages for %s, got %d", filepath.Base(item.OutputPath), got)
		}
	}
}

type pdfaOnlyProbe struct {
	checkErr error
}

func (p pdfaOnlyProbe) Check(context.Context) error { return p.checkErr }

func (p pdfaOnlyProbe) CheckPDFA(context.Context) error { return nil }

func TestDOCXPDFAPlanOnlyRequiresLibreOffice(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	probe := pdfaOnlyProbe{checkErr: &engine.DOCXError{
		Code:    engine.ErrorCodeDocxRuntimeFallbackMissing,
		Message: "pandoc binary not found in PATH",
	}}
	tool := NewDOCXToPDFToolWithDeps(probe, nil)

	pdfaPlan, pdfaErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", map[string]any{"pdfa": true}))
	if pdfaErr != nil {
		t.Fatalf("expected pdf/a plan to use only libreoffice, got %v", pdfaErr)
	}
	if !pdfaPlan.Success {
		t.Fatal("expected pdf/a plan success")
	}

	plainPlan, plainErr := tool.Plan(context.Background(), docxPlanRequest("single", []string{input}, "", nil))
	if plainErr == nil {
		t.Fatal("expected non-pdf/a plan to require pandoc")
	}
	if !strings.Contains(plainErr.Message, "pandoc") {
		t.Fatalf("expected pandoc runtime error, got %q", plainErr.Message)
	}
	if plainPlan.Success {
		t.Fatal("expected plain plan failure")
	}
}

func docxArgValue(args []string, flag string) string {
	for index := 0; index < len(args); index++ {
		if args[index] == flag && index+1 < len(args) {
			return args[index+1]
		}
	}

	return ""
}

func TestDOCXExecutePlanHonorsMutatedOutputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newPDFProducingDOCXRunner(t, 2))
	req := docxPlanRequest("single", []string{input}, "", nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	original := plan.Outputs[0].OutputPath

	mutated := filepath.Join(dir, "mutated.pdf")
	plan.Outputs[0].OutputPath = mutated

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("expected execution to succeed, got %v", execErr)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("expected one successful item, got %+v", items)
	}
	if items[0].OutputPath != mutated {
		t.Fatalf("expected the mutated output %q, got %q", mutated, items[0].OutputPath)
	}
	if _, statErr := os.Stat(mutated); statErr != nil {
		t.Fatalf("expected the mutated output to exist: %v", statErr)
	}
	if _, statErr := os.Stat(original); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the original planned output to stay absent, got %v", statErr)
	}
}

func TestDOCXExecutePlanProcessesOnlyPlannedOutputs(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.docx")
	second := filepath.Join(dir, "second.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newPDFProducingDOCXRunner(t, 2))
	req := docxPlanRequest("batch", []string{first, second}, "", nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	subset := plan
	subset.Outputs = plan.Outputs[1:]

	items, execErr := tool.ExecutePlan(context.Background(), req, subset, nil)
	if execErr != nil {
		t.Fatalf("expected subset execution to succeed, got %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected one result item, got %d", len(items))
	}
	if items[0].InputPath != second {
		t.Fatalf("expected the subset input %q, got %q", second, items[0].InputPath)
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); statErr != nil {
		t.Fatalf("expected subset output to exist: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the unplanned output to stay absent, got %v", statErr)
	}
}

func TestDOCXExecutePlanBatchRunsInParallelAndPreservesPlanOrder(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.docx")
	second := filepath.Join(dir, "second.docx")
	writePlainDOCXFixture(t, first)
	writePlainDOCXFixture(t, second)

	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	secondDone := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstOnce, secondOnce, secondDoneOnce, releaseOnce sync.Once

	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }

	runner := stubDOCXRunner{run: func(_ context.Context, name string, args ...string) (engine.CommandResult, error) {
		inputPath := ""
		if len(args) > 0 {
			inputPath = args[len(args)-1]
		}

		switch name {
		case "libreoffice":
			switch filepath.Base(inputPath) {
			case "first.docx":
				firstOnce.Do(func() { close(firstStarted) })
				<-releaseFirst
				return engine.CommandResult{Stderr: "libreoffice exploded"}, errors.New("libreoffice exploded")
			case "second.docx":
				secondOnce.Do(func() { close(secondStarted) })
				outDir := docxArgValue(args, "--outdir")
				writeDOCXPDFFixture(t, filepath.Join(outDir, "second.pdf"), 1)
				secondDoneOnce.Do(func() { close(secondDone) })
				return engine.CommandResult{}, nil
			}
		case "pandoc":
			return engine.CommandResult{Stderr: "pandoc exploded"}, errors.New("pandoc exploded")
		}

		return engine.CommandResult{}, fmt.Errorf("unexpected command %q with args %v", name, args)
	}}

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, runner)
	req := docxPlanRequest("batch", []string{first, second}, "", nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type batchOutcome struct {
		items []models.JobResultItem
		err   *models.JobError
	}
	done := make(chan batchOutcome, 1)
	go func() {
		items, err := tool.ExecutePlan(ctx, req, plan, nil)
		done <- batchOutcome{items: items, err: err}
	}()

	waitStarted := func(ch <-chan struct{}, label string) bool {
		select {
		case <-ch:
			return true
		case <-time.After(5 * time.Second):
			return false
		}
	}

	if !waitStarted(firstStarted, "first") {
		release()
		<-done
		t.Fatal("first conversion never started")
	}
	if !waitStarted(secondStarted, "second") {
		release()
		<-done
		t.Fatal("second conversion did not start while the first was running; conversions are not parallel")
	}

	// Let the second conversion finish before releasing the first one. Result
	// order must still follow the plan, not completion order.
	<-secondDone
	release()
	outcome := <-done

	if outcome.err == nil {
		t.Fatal("expected the first item failure to be reported")
	}
	if len(outcome.items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(outcome.items))
	}
	if outcome.items[0].Success {
		t.Fatalf("expected the first item to fail, got %+v", outcome.items[0])
	}
	if !outcome.items[1].Success {
		t.Fatalf("expected the second item to succeed, got %+v", outcome.items[1])
	}
	if outcome.items[0].InputPath != first || outcome.items[1].InputPath != second {
		t.Fatalf("expected plan order, got %+v", outcome.items)
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); statErr != nil {
		t.Fatalf("expected the successful output to exist: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the failed output to stay absent, got %v", statErr)
	}
}

func TestDOCXExecutePlanUsesPlannedEffectiveOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writePlainDOCXFixture(t, input)

	tool := NewDOCXToPDFToolWithDeps(stubDOCXRuntimeProbe{}, newPDFProducingDOCXRunner(t, 5))
	req := docxPlanRequest("single", []string{input}, "", map[string]any{"pageSelection": "2-4"})
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	// ExecutePlan must trust the plan, not the request that produced it.
	plan.EffectiveOptions = map[string]any{"pageSelection": "1"}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("expected execution to succeed, got %v", execErr)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("expected one successful item, got %+v", items)
	}
	if got := docxPDFPageCount(t, items[0].OutputPath); got != 1 {
		t.Fatalf("expected the plan page selection to keep 1 page, got %d", got)
	}
}
