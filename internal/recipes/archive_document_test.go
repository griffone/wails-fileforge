package recipes

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/doc"
	docengine "fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/registry"
)

// requireArchiveGhostscript skips a run test when the machine cannot run the
// real PDF/A step. Plan tests inject the detector instead.
func requireArchiveGhostscript(t *testing.T) {
	t.Helper()

	if !archiveGhostscriptAvailable() {
		t.Skip("ghostscript is not installed; skipping the PDF/A archive run")
	}
}

// stubArchiveGhostscriptAvailable replaces the plan-time detector for a test.
func stubArchiveGhostscriptAvailable(t *testing.T, available bool) {
	t.Helper()

	original := archiveGhostscriptAvailable
	archiveGhostscriptAvailable = func() bool { return available }
	t.Cleanup(func() {
		archiveGhostscriptAvailable = original
	})
}

func newArchiveTestService(t *testing.T) (*Service, *jobhistory.Store, *eventRecorder) {
	t.Helper()

	return newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, pdf.NewMergeTool())
		mustRegisterTool(t, reg, pdf.NewOptimizeTool())
		mustRegisterTool(t, reg, doc.NewDOCXToPDFTool())
	})
}

// writeArchiveDOCXFixture writes a minimal valid OOXML package with one page
// break per extra page, so the renderer produces exactly the requested pages.
func writeArchiveDOCXFixture(t *testing.T, path string, pages int) {
	t.Helper()

	if pages < 1 {
		pages = 1
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}

	archive := zip.NewWriter(file)
	writeArchiveZipEntry(t, archive, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	writeArchiveZipEntry(t, archive, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	var body strings.Builder
	for page := 1; page <= pages; page++ {
		body.WriteString("<w:p><w:r>")
		if page > 1 {
			body.WriteString(`<w:br w:type="page"/>`)
		}
		fmt.Fprintf(&body, `<w:t>Archive page %d</w:t></w:r></w:p>`, page)
	}
	writeArchiveZipEntry(t, archive, "word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>`+body.String()+`
<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>
</w:body>
</w:document>`)

	if err := archive.Close(); err != nil {
		t.Fatalf("close docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}

func writeArchiveZipEntry(t *testing.T, archive *zip.Writer, name, content string) {
	t.Helper()

	entry, err := archive.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}

// writeBrokenArchiveDOCXFixture writes a valid ZIP package without the document
// part, so planning accepts it and the renderer fails at execution.
func writeBrokenArchiveDOCXFixture(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create broken docx: %v", err)
	}

	archive := zip.NewWriter(file)
	writeArchiveZipEntry(t, archive, "word/settings.xml", "<w:settings/>")
	if err := archive.Close(); err != nil {
		t.Fatalf("close broken docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close broken docx: %v", err)
	}
}

func archiveSHA256Of(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	digest := sha256.Sum256(content)

	return hex.EncodeToString(digest[:])
}

func archiveCheckByKey(report models.VerificationReport, key string) *models.VerificationCheck {
	for outputIndex := range report.Outputs {
		for checkIndex := range report.Outputs[outputIndex].Checks {
			if report.Outputs[outputIndex].Checks[checkIndex].Key == key {
				return &report.Outputs[outputIndex].Checks[checkIndex]
			}
		}
	}

	return nil
}

func TestPlanArchiveDocumentPlansPDFAOutput(t *testing.T) {
	stubArchiveGhostscriptAvailable(t, true)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 2)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if plan.Recipe == nil || plan.Recipe.ID != RecipeIDArchiveDocument {
		t.Fatalf("unexpected recipe metadata: %+v", plan.Recipe)
	}
	if plan.OutputDir != dir {
		t.Fatalf("expected the output folder to default to the input folder, got %q", plan.OutputDir)
	}

	wantPath := filepath.Join(dir, "report_archived.pdf")
	if len(plan.Outputs) != 1 || plan.Outputs[0].OutputPath != wantPath {
		t.Fatalf("expected one planned output %q, got %+v", wantPath, plan.Outputs)
	}
	if !warningsContain(plan.Warnings, "not validated with veraPDF") {
		t.Fatalf("expected the compliance caveat in the plan warnings, got %+v", plan.Warnings)
	}
}

func TestPlanArchiveDocumentSidecarOption(t *testing.T) {
	stubArchiveGhostscriptAvailable(t, true)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 1)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
		Options:    map[string]any{archiveSidecarOptionKey: true},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}

	wantPDF := filepath.Join(dir, "report_archived.pdf")
	wantSidecar := filepath.Join(dir, "report_archived.pdf.sha256")
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected the PDF and its sidecar, got %+v", plan.Outputs)
	}
	if plan.Outputs[0].OutputPath != wantPDF || plan.Outputs[1].OutputPath != wantSidecar {
		t.Fatalf("unexpected planned outputs: %+v", plan.Outputs)
	}
}

func TestPlanArchiveDocumentRejectsInvalidSidecarOption(t *testing.T) {
	stubArchiveGhostscriptAvailable(t, true)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 1)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
		Options:    map[string]any{archiveSidecarOptionKey: "yes"},
	})
	if plan.Success {
		t.Fatal("expected a non-boolean sidecar option to fail the plan")
	}
	if plan.Error == nil || plan.Error.DetailCode != "RECIPE_OPTION_INVALID_TYPE" {
		t.Fatalf("expected a canonical option error, got %+v", plan.Error)
	}
}

func TestPlanArchiveDocumentCollisionSafe(t *testing.T) {
	stubArchiveGhostscriptAvailable(t, true)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 1)

	existing := filepath.Join(dir, "report_archived.pdf")
	if err := os.WriteFile(existing, []byte("%PDF-1.7 existing"), 0o644); err != nil {
		t.Fatalf("write colliding output: %v", err)
	}

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
		Options:    map[string]any{archiveSidecarOptionKey: true},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}

	wantPDF := filepath.Join(dir, "report_archived-2.pdf")
	wantSidecar := filepath.Join(dir, "report_archived-2.pdf.sha256")
	if len(plan.Outputs) != 2 || plan.Outputs[0].OutputPath != wantPDF || plan.Outputs[1].OutputPath != wantSidecar {
		t.Fatalf("expected the collision-safe names %q and %q, got %+v", wantPDF, wantSidecar, plan.Outputs)
	}
	if !warningsContain(plan.Warnings, "already exists") {
		t.Fatalf("expected a collision warning, got %+v", plan.Warnings)
	}
}

func TestPlanArchiveDocumentRequiresGhostscript(t *testing.T) {
	stubArchiveGhostscriptAvailable(t, false)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 1)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
	})
	if plan.Success {
		t.Fatal("expected the plan to fail without Ghostscript")
	}
	if plan.Error == nil {
		t.Fatal("expected a typed dependency error")
	}
	if plan.Error.DetailCode != docengine.ErrorCodeDocxPDFAGhostscriptMissing {
		t.Fatalf("expected %s, got %q", docengine.ErrorCodeDocxPDFAGhostscriptMissing, plan.Error.DetailCode)
	}
	if plan.Error.Code != models.ErrorCodeRuntimeDepMissing {
		t.Fatalf("expected the runtime dependency family, got %q", plan.Error.Code)
	}
	if !strings.Contains(plan.Error.Message, "Ghostscript") || !strings.Contains(plan.Error.Message, "Install") {
		t.Fatalf("expected install guidance, got %q", plan.Error.Message)
	}
	for _, candidate := range []string{"gs", "gswin64c", "gswin32c"} {
		if !strings.Contains(plan.Error.Message, candidate) {
			t.Fatalf("expected the error to list %q, got %q", candidate, plan.Error.Message)
		}
	}

	// Nothing was written before the dependency check.
	assertDirContainsOnly(t, dir, "report.docx")
}

func TestPlanArchiveDocumentRejectsInvalidRequests(t *testing.T) {
	stubArchiveGhostscriptAvailable(t, true)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 1)
	other := filepath.Join(dir, "second.docx")
	writeArchiveDOCXFixture(t, other, 1)
	notDOCX := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, notDOCX, 1)

	cases := []struct {
		name   string
		inputs []string
		detail string
	}{
		{name: "no inputs", inputs: nil, detail: "RECIPE_INVALID_INPUT"},
		{name: "two inputs", inputs: []string{input, other}, detail: "RECIPE_INVALID_INPUT"},
		{name: "not docx", inputs: []string{notDOCX}, detail: "RECIPE_INVALID_INPUT"},
		{name: "missing file", inputs: []string{filepath.Join(dir, "gone.docx")}, detail: "RECIPE_INPUT_MISSING"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := service.Plan(models.RecipeRequest{
				RecipeID:   RecipeIDArchiveDocument,
				InputPaths: testCase.inputs,
			})
			if plan.Success {
				t.Fatal("expected the plan to fail")
			}
			if plan.Error == nil || plan.Error.DetailCode != testCase.detail {
				t.Fatalf("expected %s, got %+v", testCase.detail, plan.Error)
			}
		})
	}
}

func TestPlanArchiveDocumentRejectsCorruptDOCX(t *testing.T) {
	stubArchiveGhostscriptAvailable(t, true)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	if err := os.WriteFile(input, []byte("not a docx package"), 0o644); err != nil {
		t.Fatalf("write corrupt docx: %v", err)
	}

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
		Options:    map[string]any{archiveSidecarOptionKey: true},
	})
	if plan.Success {
		t.Fatal("expected a corrupt DOCX to fail the plan")
	}
	if plan.Error == nil || plan.Error.Code == "" || plan.Error.Code == models.ErrorCodeUnclassified {
		t.Fatalf("expected a canonical error, got %+v", plan.Error)
	}
	assertDirContainsOnly(t, dir, "report.docx")
}

func TestRunArchiveDocumentProducesPDFAAndChecksum(t *testing.T) {
	requireArchiveGhostscript(t)
	service, history, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 2)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %s: %s (%+v)", result.Status, result.Message, result.Error)
	}
	if result.Verification == nil || !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}

	pdfPath := filepath.Join(dir, "report_archived.pdf")
	if len(result.Outputs) != 1 || result.Outputs[0] != pdfPath {
		t.Fatalf("expected only the PDF, got %+v", result.Outputs)
	}
	if _, statErr := os.Stat(pdfPath + ".sha256"); !os.IsNotExist(statErr) {
		t.Fatalf("expected no sidecar with the option off, stat err = %v", statErr)
	}

	// The required chain: file, magic, PDF/A marker, page count, checksum.
	wantKeys := []string{
		archivePDFFileCheck,
		archivePDFMagicCheck,
		archivePDFAMarkerCheck,
		archivePDFAPageCountCheck,
		archiveSHA256Check,
	}
	checks := result.Verification.Outputs[0].Checks
	if len(checks) != len(wantKeys) {
		t.Fatalf("expected %d checks, got %+v", len(wantKeys), checks)
	}
	for index, key := range wantKeys {
		if checks[index].Key != key {
			t.Fatalf("expected check %d to be %q, got %q", index, key, checks[index].Key)
		}
		if !checks[index].Passed {
			t.Fatalf("expected check %q to pass, got %+v", key, checks[index])
		}
	}

	if version := archiveCheckByKey(*result.Verification, archivePDFMagicCheck).Value; version != "%PDF-1.7" {
		t.Fatalf("expected a PDF 1.7 signature, got %q", version)
	}
	marker := archiveCheckByKey(*result.Verification, archivePDFAMarkerCheck)
	if marker.Value != "PDF/A-2B" {
		t.Fatalf("expected the PDF/A-2B marker, got %+v", marker)
	}
	if pages := archiveCheckByKey(*result.Verification, archivePDFAPageCountCheck).Value; pages != "2 pages" {
		t.Fatalf("expected two pages, got %q", pages)
	}

	wantHash := archiveSHA256Of(t, pdfPath)
	if got := archiveCheckByKey(*result.Verification, archiveSHA256Check).Value; got != wantHash {
		t.Fatalf("reported hash %q does not match sha256sum %q", got, wantHash)
	}
	if len(result.Steps) != 2 || result.Steps[1].Status != recipeStepStatusSuccess {
		t.Fatalf("expected both steps to succeed, got %+v", result.Steps)
	}
	for _, message := range []string{result.Steps[1].Message, result.Message} {
		if !strings.Contains(message, "not validated with veraPDF") {
			t.Fatalf("expected the compliance caveat in %q", message)
		}
	}
	if !strings.Contains(result.Message, wantHash) {
		t.Fatalf("expected the checksum in the result message, got %q", result.Message)
	}

	entries := history.List()
	if len(entries) != 1 || entries[0].ToolID != archiveDocumentHistoryToolID || len(entries[0].Outputs) != 1 {
		t.Fatalf("unexpected history entries: %+v", entries)
	}
}

func TestRunArchiveDocumentWritesSidecar(t *testing.T) {
	requireArchiveGhostscript(t)
	service, _, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.docx")
	writeArchiveDOCXFixture(t, input, 1)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
		Options:    map[string]any{archiveSidecarOptionKey: true},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %s: %s (%+v)", result.Status, result.Message, result.Error)
	}

	pdfPath := filepath.Join(dir, "report_archived.pdf")
	sidecarPath := pdfPath + ".sha256"
	if len(result.Outputs) != 2 || result.Outputs[1] != sidecarPath {
		t.Fatalf("expected the PDF and its sidecar, got %+v", result.Outputs)
	}

	hash := archiveSHA256Of(t, pdfPath)
	content, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	want := hash + "  " + filepath.Base(pdfPath) + "\n"
	if string(content) != want {
		t.Fatalf("expected sha256sum line %q, got %q", want, string(content))
	}
	if result.Verification == nil || len(result.Verification.Outputs) != 2 {
		t.Fatalf("expected the sidecar in the verification report, got %+v", result.Verification)
	}
}

func TestRunArchiveDocumentUserRecipeSidecar(t *testing.T) {
	requireArchiveGhostscript(t)
	service, _, _ := newArchiveTestService(t)

	saved := service.SaveUserRecipe(models.SaveUserRecipeRequest{
		Name:         "Archive with sidecar",
		BaseRecipeID: RecipeIDArchiveDocument,
		Options:      map[string]any{archiveSidecarOptionKey: true},
	})
	if !saved.Success || saved.Recipe == nil {
		t.Fatalf("expected the user recipe to be saved, got %+v", saved)
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "contract.docx")
	writeArchiveDOCXFixture(t, input, 1)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   saved.Recipe.ID,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the duplicating recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %s: %s (%+v)", result.Status, result.Message, result.Error)
	}

	sidecarPath := filepath.Join(dir, "contract_archived.pdf.sha256")
	if len(result.Outputs) != 2 || result.Outputs[1] != sidecarPath {
		t.Fatalf("expected the saved sidecar option to apply, got %+v", result.Outputs)
	}
	if _, statErr := os.Stat(sidecarPath); statErr != nil {
		t.Fatalf("expected the sidecar to exist: %v", statErr)
	}
	if result.RecipeID != saved.Recipe.ID {
		t.Fatalf("expected the run to report the user recipe id, got %q", result.RecipeID)
	}
}

func TestRunArchiveDocumentBrokenDOCXLeavesNoOutputs(t *testing.T) {
	requireArchiveGhostscript(t)
	service, history, _ := newArchiveTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "broken.docx")
	writeBrokenArchiveDOCXFixture(t, input)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDArchiveDocument,
		InputPaths: []string{input},
		Options:    map[string]any{archiveSidecarOptionKey: true},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failure, got %s: %s", result.Status, result.Message)
	}
	if result.Error == nil || result.Error.Code == "" || result.Error.Code == models.ErrorCodeUnclassified {
		t.Fatalf("expected a canonical error, got %+v", result.Error)
	}

	assertDirContainsOnly(t, dir, "broken.docx")
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}
}
