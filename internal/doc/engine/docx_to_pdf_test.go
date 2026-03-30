package engine

import (
	"archive/zip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeDOCXRunner struct {
	runs []fakeDOCXRun
	idx  int
	args [][]string
	name []string
}

type fakeDOCXRun struct {
	result CommandResult
	err    error
	hook   func(name string, args ...string)
}

func (f *fakeDOCXRunner) Run(_ context.Context, name string, args ...string) (CommandResult, error) {
	f.name = append(f.name, name)
	f.args = append(f.args, append([]string{}, args...))
	if f.idx >= len(f.runs) {
		return CommandResult{}, nil
	}
	r := f.runs[f.idx]
	f.idx++
	if r.hook != nil {
		r.hook(name, args...)
	}
	return r.result, r.err
}

type fakeProbe struct {
	err error
}

func (p fakeProbe) Check(_ context.Context) error {
	return p.err
}

func TestConvertDOCX_FallbackOnPrimaryError(t *testing.T) {
	tmpDir := t.TempDir()
	input := createDOCXFixture(t, tmpDir, false)
	output := filepath.Join(tmpDir, "output.pdf")

	runner := &fakeDOCXRunner{runs: []fakeDOCXRun{
		{err: errors.New("libreoffice failed"), result: CommandResult{Stderr: "runtime error"}},
		{hook: func(name string, args ...string) { _ = os.WriteFile(output, []byte("%PDF-1.4\n"), 0o644) }},
	}}

	res, err := ConvertDOCX(context.Background(), fakeProbe{}, runner, ConvertDOCXRequest{InputPath: input, OutputPath: output})
	if err != nil {
		t.Fatalf("expected fallback success, got err: %v", err)
	}
	if !res.FallbackUsed || res.EngineUsed != EnginePandoc {
		t.Fatalf("expected pandoc fallback result, got %+v", res)
	}
	if len(runner.name) != 2 || runner.name[0] != "libreoffice" || runner.name[1] != "pandoc" {
		t.Fatalf("expected libreoffice then pandoc, got %#v", runner.name)
	}
}

func TestConvertDOCX_FontDetectionErrorEvenAfterFallback(t *testing.T) {
	tmpDir := t.TempDir()
	input := createDOCXFixture(t, tmpDir, false)
	output := filepath.Join(tmpDir, "output.pdf")

	runner := &fakeDOCXRunner{runs: []fakeDOCXRun{
		{result: CommandResult{Stdout: "Font substitute applied"}},
		{hook: func(name string, args ...string) { _ = os.WriteFile(output, []byte("%PDF-1.4\n"), 0o644) }},
	}}

	_, err := ConvertDOCX(context.Background(), fakeProbe{}, runner, ConvertDOCXRequest{InputPath: input, OutputPath: output})
	if err == nil {
		t.Fatalf("expected font detection error")
	}
	var docxErr *DOCXError
	if !errors.As(err, &docxErr) {
		t.Fatalf("expected DOCXError, got %T", err)
	}
	if docxErr.Code != ErrorCodeDocxFontSubstitution {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxFontSubstitution, docxErr.Code)
	}
}

func TestDetectProtectedDOCX_Rejected(t *testing.T) {
	tmpDir := t.TempDir()
	input := createDOCXFixture(t, tmpDir, true)

	err := DetectProtectedDOCX(input)
	if err == nil {
		t.Fatalf("expected protected docx rejection")
	}
	var docxErr *DOCXError
	if !errors.As(err, &docxErr) {
		t.Fatalf("expected DOCXError, got %T", err)
	}
	if docxErr.Code != ErrorCodeDocxInputProtected {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxInputProtected, docxErr.Code)
	}
}

func TestDetectFontIssue(t *testing.T) {
	err := detectFontIssue("warning: font not found, substitution in fallback")
	if err == nil {
		t.Fatalf("expected font detection error")
	}
	var docxErr *DOCXError
	if !errors.As(err, &docxErr) {
		t.Fatalf("expected DOCXError, got %T", err)
	}
	if docxErr.Code != ErrorCodeDocxFontSubstitution {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxFontSubstitution, docxErr.Code)
	}
}

func createDOCXFixture(t *testing.T, dir string, protected bool) string {
	t.Helper()

	path := filepath.Join(dir, "input.docx")
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
		t.Fatalf("close file: %v", err)
	}

	if !strings.HasSuffix(path, ".docx") {
		t.Fatalf("invalid fixture suffix")
	}

	return path
}
