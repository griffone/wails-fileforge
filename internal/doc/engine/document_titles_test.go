package engine

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func writeDOCXWithParts(t *testing.T, path string, parts map[string]string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}

	archive := zip.NewWriter(file)
	for name, content := range parts {
		writeZipEntry(t, archive, name, content)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}

func readDOCXTitlesFromPath(t *testing.T, path string) DOCXTitles {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open docx: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat docx: %v", err)
	}

	titles, err := ReadDOCXTitles(file, info.Size())
	if err != nil {
		t.Fatalf("ReadDOCXTitles: %v", err)
	}

	return titles
}

func TestReadDOCXTitlesPrefersTheCoreTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "titled.docx")
	writeDOCXWithParts(t, path, map[string]string{
		"docProps/core.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/">
<dc:title>Annual Budget</dc:title>
</cp:coreProperties>`,
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Ignored heading</w:t></w:r></w:p>
</w:body></w:document>`,
	})

	titles := readDOCXTitlesFromPath(t, path)
	if titles.CoreTitle != "Annual Budget" {
		t.Fatalf("core title = %q, want %q", titles.CoreTitle, "Annual Budget")
	}
}

func TestReadDOCXTitlesFallsBackToTheFirstHeading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heading.docx")
	writeDOCXWithParts(t, path, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>Cover line</w:t></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Project </w:t></w:r><w:r><w:t>Plan</w:t></w:r></w:p>
</w:body></w:document>`,
	})

	titles := readDOCXTitlesFromPath(t, path)
	if titles.CoreTitle != "" {
		t.Fatalf("core title = %q, want empty", titles.CoreTitle)
	}
	if titles.Heading != "Project Plan" {
		t.Fatalf("heading = %q, want %q", titles.Heading, "Project Plan")
	}
}

func TestReadDOCXTitlesIgnoresHeadingsInsideTextBoxes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested.docx")
	writeDOCXWithParts(t, path, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:pict><w:txbxContent><w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Caption</w:t></w:r></w:p></w:txbxContent></w:pict></w:r></w:p>
<w:p><w:pPr><w:pStyle w:val="Heading2"/></w:pPr><w:r><w:t>Real section</w:t></w:r></w:p>
</w:body></w:document>`,
	})

	titles := readDOCXTitlesFromPath(t, path)
	if titles.Heading != "Real section" {
		t.Fatalf("heading = %q, want %q", titles.Heading, "Real section")
	}
}

func TestReadDOCXTitlesRejectsABrokenPackage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.docx")
	if err := os.WriteFile(path, []byte("not a zip"), 0o644); err != nil {
		t.Fatalf("write broken docx: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open broken docx: %v", err)
	}
	defer file.Close()

	if _, err := ReadDOCXTitles(file, int64(len("not a zip"))); err == nil {
		t.Fatal("ReadDOCXTitles returned no error for a broken package")
	}
}

func TestReadMarkdownTitleReturnsTheFirstLevelOneHeading(t *testing.T) {
	source := []byte("# Launch Notes\n\n## Summary\n\n# Later heading\n")

	title, err := ReadMarkdownTitle(source)
	if err != nil {
		t.Fatalf("ReadMarkdownTitle: %v", err)
	}
	if title != "Launch Notes" {
		t.Fatalf("title = %q, want %q", title, "Launch Notes")
	}
}

func TestReadMarkdownTitleIgnoresHeadingsInsideCodeFences(t *testing.T) {
	source := []byte("```sh\n# install\n```\n\n# Real title\n")

	title, err := ReadMarkdownTitle(source)
	if err != nil {
		t.Fatalf("ReadMarkdownTitle: %v", err)
	}
	if title != "Real title" {
		t.Fatalf("title = %q, want %q", title, "Real title")
	}
}

func TestReadMarkdownTitleReturnsEmptyWithoutHeading(t *testing.T) {
	title, err := ReadMarkdownTitle([]byte("just a paragraph\n\n## only level two\n"))
	if err != nil {
		t.Fatalf("ReadMarkdownTitle: %v", err)
	}
	if title != "" {
		t.Fatalf("title = %q, want empty", title)
	}
}
