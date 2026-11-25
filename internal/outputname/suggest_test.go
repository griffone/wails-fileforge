package outputname

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"

	"fileforge-desktop/internal/platform/fsx"
)

const toolID = "tool.pdf.merge"

func writePDF(t *testing.T, path, title string) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.AddPage()
	document.SetFont("Helvetica", "", 12)
	document.Cell(40, 10, "Body text")
	if title != "" {
		document.SetTitle(title, true)
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
}

func writeDOCX(t *testing.T, path string, parts map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}

	archive := zip.NewWriter(file)
	for name, content := range parts {
		entry, entryErr := archive.Create(name)
		if entryErr != nil {
			t.Fatalf("create zip entry %s: %v", name, entryErr)
		}
		if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
			t.Fatalf("write zip entry %s: %v", name, writeErr)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func requireSuggestion(t *testing.T, suggestion Suggestion) {
	t.Helper()

	if !suggestion.Found {
		t.Fatalf("suggestion not found: reason = %q", suggestion.Reason)
	}
	if suggestion.SuggestedName == "" {
		t.Fatal("suggestion has an empty name")
	}
}

func TestSuggestUsesThePDFTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.pdf")
	writePDF(t, path, "Quarterly Report")

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)
	if suggestion.SuggestedName != "Quarterly Report" {
		t.Fatalf("name = %q, want %q", suggestion.SuggestedName, "Quarterly Report")
	}
	if suggestion.Source != SourcePDFTitle {
		t.Fatalf("source = %q, want %q", suggestion.Source, SourcePDFTitle)
	}
}

func TestSuggestFallsBackToTheInputNameForAnUntitledPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Q1___draft--notes.pdf")
	writePDF(t, path, "")

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)
	if suggestion.Source != SourceInputName {
		t.Fatalf("source = %q, want %q", suggestion.Source, SourceInputName)
	}
	if suggestion.SuggestedName != "Q1 draft notes" {
		t.Fatalf("name = %q, want %q", suggestion.SuggestedName, "Q1 draft notes")
	}
}

func TestSuggestUsesTheDOCXCoreTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.docx")
	writeDOCX(t, path, map[string]string{
		"docProps/core.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>Annual Budget</dc:title>
</cp:coreProperties>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Ignored</w:t></w:r></w:p>
</w:body></w:document>`,
	})

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)
	if suggestion.SuggestedName != "Annual Budget" {
		t.Fatalf("name = %q, want %q", suggestion.SuggestedName, "Annual Budget")
	}
	if suggestion.Source != SourceDOCXTitle {
		t.Fatalf("source = %q, want %q", suggestion.Source, SourceDOCXTitle)
	}
}

func TestSuggestUsesTheDOCXFirstHeading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.docx")
	writeDOCX(t, path, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>Cover</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Project Plan</w:t></w:r></w:p>
</w:body></w:document>`,
	})

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)
	if suggestion.SuggestedName != "Project Plan" {
		t.Fatalf("name = %q, want %q", suggestion.SuggestedName, "Project Plan")
	}
	if suggestion.Source != SourceDOCXHeading {
		t.Fatalf("source = %q, want %q", suggestion.Source, SourceDOCXHeading)
	}
}

func TestSuggestUsesTheMarkdownFirstHeading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	writeFile(t, path, "```\n# install\n```\n\n# Launch Notes\n\nBody.\n")

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)
	if suggestion.SuggestedName != "Launch Notes" {
		t.Fatalf("name = %q, want %q", suggestion.SuggestedName, "Launch Notes")
	}
	if suggestion.Source != SourceMDHeading {
		t.Fatalf("source = %q, want %q", suggestion.Source, SourceMDHeading)
	}
}

func TestSuggestFallsBackToTheInputNameForPlainFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meeting_notes.txt")
	writeFile(t, path, "no metadata here")

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)
	if suggestion.SuggestedName != "meeting notes" {
		t.Fatalf("name = %q, want %q", suggestion.SuggestedName, "meeting notes")
	}
	if suggestion.Source != SourceInputName {
		t.Fatalf("source = %q, want %q", suggestion.Source, SourceInputName)
	}
}

func TestSuggestSanitizesWindowsIllegalCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.pdf")
	writePDF(t, path, `Report: Q1/Q2 "final" <draft>|v1? *`)

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)

	if strings.ContainsAny(suggestion.SuggestedName, `<>:"/\|?*`) {
		t.Fatalf("name %q still contains characters illegal on Windows", suggestion.SuggestedName)
	}
	if suggestion.SuggestedName != "Report Q1 Q2 final draft v1" {
		t.Fatalf("name = %q, want %q", suggestion.SuggestedName, "Report Q1 Q2 final draft v1")
	}
}

func TestSuggestCapsAVeryLongTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.pdf")
	writePDF(t, path, strings.Repeat("Long title ", 40))

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	requireSuggestion(t, suggestion)

	if runeCount := len([]rune(suggestion.SuggestedName)); runeCount != fsx.MaxBaseNameLength {
		t.Fatalf("name length = %d, want %d", runeCount, fsx.MaxBaseNameLength)
	}
}

func TestSuggestReportsMissingInput(t *testing.T) {
	_, err := Suggest(toolID, filepath.Join(t.TempDir(), "missing.pdf"))
	if err == nil {
		t.Fatal("Suggest returned no error for a missing file")
	}
	if err.Code != ErrorCodeInvalidPath {
		t.Fatalf("code = %q, want %q", err.Code, ErrorCodeInvalidPath)
	}
}

func TestSuggestReportsADirectory(t *testing.T) {
	_, err := Suggest(toolID, t.TempDir())
	if err == nil {
		t.Fatal("Suggest returned no error for a directory")
	}
	if err.Code != ErrorCodeInvalidPath {
		t.Fatalf("code = %q, want %q", err.Code, ErrorCodeInvalidPath)
	}
}

func TestSuggestReportsAnUndecodablePDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.pdf")
	writeFile(t, path, "not a pdf")

	_, err := Suggest(toolID, path)
	if err == nil {
		t.Fatal("Suggest returned no error for a broken PDF")
	}
	if err.Code != ErrorCodeDecodeFailed {
		t.Fatalf("code = %q, want %q", err.Code, ErrorCodeDecodeFailed)
	}
}

func TestSuggestReportsAnUndecodableDOCX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.docx")
	writeFile(t, path, "not a zip")

	_, err := Suggest(toolID, path)
	if err == nil {
		t.Fatal("Suggest returned no error for a broken DOCX")
	}
	if err.Code != ErrorCodeDecodeFailed {
		t.Fatalf("code = %q, want %q", err.Code, ErrorCodeDecodeFailed)
	}
}

func TestSuggestReportsAnUnreadableInput(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits are not enforced")
	}

	path := filepath.Join(t.TempDir(), "secret.md")
	writeFile(t, path, "# Hidden")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := Suggest(toolID, path)
	if err == nil {
		t.Fatal("Suggest returned no error for an unreadable file")
	}
	if err.Code != ErrorCodeReadFailed {
		t.Fatalf("code = %q, want %q", err.Code, ErrorCodeReadFailed)
	}
}

func TestSuggestReportsNoUsableName(t *testing.T) {
	path := filepath.Join(t.TempDir(), `<>:"\|?*.txt`)
	writeFile(t, path, "plain")

	suggestion, err := Suggest(toolID, path)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if suggestion.Found {
		t.Fatalf("suggestion found = true with name %q, want false", suggestion.SuggestedName)
	}
	if suggestion.Reason == "" {
		t.Fatal("a not-found suggestion must explain why")
	}
}
