package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	EngineLibreOffice = "libreoffice"
	EnginePandoc      = "pandoc"
)

const (
	ErrorCodeDocxRuntimePrimaryMissing  = "DOC_DOCX_TO_PDF_RUNTIME_LIBREOFFICE_MISSING"
	ErrorCodeDocxRuntimeFallbackMissing = "DOC_DOCX_TO_PDF_RUNTIME_PANDOC_MISSING"
	ErrorCodeDocxInputProtected         = "DOC_DOCX_TO_PDF_INPUT_PROTECTED"
	ErrorCodeDocxInputEncrypted         = "DOC_DOCX_TO_PDF_INPUT_ENCRYPTED"
	ErrorCodeDocxFontSubstitution       = "DOC_DOCX_TO_PDF_FONTS_MISSING_OR_SUBSTITUTED"
	ErrorCodeDocxPrimaryExecution       = "DOC_DOCX_TO_PDF_PRIMARY_EXECUTION_FAILED"
	ErrorCodeDocxFallbackExecution      = "DOC_DOCX_TO_PDF_FALLBACK_EXECUTION_FAILED"
)

type CommandResult struct {
	Stdout string
	Stderr string
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (CommandResult, error)
}

type RuntimeProbe interface {
	Check(ctx context.Context) error
}

type ConvertDOCXRequest struct {
	InputPath  string
	OutputPath string
}

type ConvertDOCXResult struct {
	EngineUsed    string
	FallbackUsed  bool
	PrimaryError  string
	FallbackError string
}

type DOCXError struct {
	Code    string
	Message string
	Cause   error
	Details map[string]any
}

func (e *DOCXError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *DOCXError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type HybridRuntimeProbe struct {
	lookupPath func(file string) (string, error)
}

func NewHybridRuntimeProbe() *HybridRuntimeProbe {
	return &HybridRuntimeProbe{lookupPath: exec.LookPath}
}

func (p *HybridRuntimeProbe) Check(_ context.Context) error {
	lookup := exec.LookPath
	if p != nil && p.lookupPath != nil {
		lookup = p.lookupPath
	}

	if _, err := lookup("libreoffice"); err != nil {
		return &DOCXError{Code: ErrorCodeDocxRuntimePrimaryMissing, Message: "libreoffice binary not found in PATH", Cause: err}
	}

	if _, err := lookup("pandoc"); err != nil {
		return &DOCXError{Code: ErrorCodeDocxRuntimeFallbackMissing, Message: "pandoc binary not found in PATH", Cause: err}
	}

	return nil
}

type ExecCommandRunner struct{}

func (r *ExecCommandRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String()}, err
}

func ConvertDOCX(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req ConvertDOCXRequest) (ConvertDOCXResult, error) {
	if probe == nil {
		probe = NewHybridRuntimeProbe()
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}

	if err := probe.Check(ctx); err != nil {
		return ConvertDOCXResult{}, err
	}

	if err := DetectProtectedDOCX(req.InputPath); err != nil {
		return ConvertDOCXResult{}, err
	}

	primaryErr := runLibreOffice(ctx, runner, req.InputPath, req.OutputPath)
	if primaryErr == nil {
		return ConvertDOCXResult{EngineUsed: EngineLibreOffice, FallbackUsed: false}, nil
	}

	fallbackErr := runPandoc(ctx, runner, req.InputPath, req.OutputPath)
	if fallbackErr == nil {
		if isFontDetectionError(primaryErr) {
			return ConvertDOCXResult{
				EngineUsed:   EnginePandoc,
				FallbackUsed: true,
				PrimaryError: primaryErr.Error(),
			}, primaryErr
		}

		return ConvertDOCXResult{
			EngineUsed:   EnginePandoc,
			FallbackUsed: true,
			PrimaryError: primaryErr.Error(),
		}, nil
	}

	return ConvertDOCXResult{
		EngineUsed:    EnginePandoc,
		FallbackUsed:  true,
		PrimaryError:  primaryErr.Error(),
		FallbackError: fallbackErr.Error(),
	}, fallbackErr
}

func runLibreOffice(ctx context.Context, runner CommandRunner, inputPath, outputPath string) error {
	tmpOutDir, err := os.MkdirTemp("", "fileforge-docx2pdf-lo-")
	if err != nil {
		return &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "unable to prepare libreoffice output dir", Cause: err}
	}
	defer func() { _ = os.RemoveAll(tmpOutDir) }()

	result, execErr := runner.Run(ctx, "libreoffice", "--headless", "--convert-to", "pdf", "--outdir", tmpOutDir, inputPath)
	if execErr != nil {
		if err := mapProtectedFromExecution(result.Stdout + "\n" + result.Stderr); err != nil {
			return err
		}
		return &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "libreoffice execution failed", Cause: execErr, Details: map[string]any{"stderr": strings.TrimSpace(result.Stderr)}}
	}

	if err := detectFontIssue(result.Stdout + "\n" + result.Stderr); err != nil {
		return err
	}

	converted := filepath.Join(tmpOutDir, strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))+".pdf")
	if _, statErr := os.Stat(converted); statErr != nil {
		return &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "libreoffice did not produce output pdf", Cause: statErr}
	}

	if mkErr := os.MkdirAll(filepath.Dir(outputPath), 0o755); mkErr != nil {
		return &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "unable to create output directory", Cause: mkErr}
	}

	if moveErr := moveFile(converted, outputPath); moveErr != nil {
		return &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "unable to finalize libreoffice output", Cause: moveErr}
	}

	return nil
}

func runPandoc(ctx context.Context, runner CommandRunner, inputPath, outputPath string) error {
	result, execErr := runner.Run(ctx, "pandoc", inputPath, "-o", outputPath)
	if execErr != nil {
		if err := mapProtectedFromExecution(result.Stdout + "\n" + result.Stderr); err != nil {
			return err
		}
		return &DOCXError{Code: ErrorCodeDocxFallbackExecution, Message: "pandoc execution failed", Cause: execErr, Details: map[string]any{"stderr": strings.TrimSpace(result.Stderr)}}
	}

	if err := detectFontIssue(result.Stdout + "\n" + result.Stderr); err != nil {
		return err
	}

	if _, err := os.Stat(outputPath); err != nil {
		return &DOCXError{Code: ErrorCodeDocxFallbackExecution, Message: "pandoc did not produce output pdf", Cause: err}
	}

	return nil
}

func DetectProtectedDOCX(inputPath string) error {
	f, err := os.Open(inputPath)
	if err != nil {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "unable to inspect docx protection metadata", Cause: err}
	}
	defer func() { _ = f.Close() }()

	fi, err := f.Stat()
	if err != nil {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "unable to inspect docx file metadata", Cause: err}
	}

	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "docx appears encrypted or invalid package", Cause: err}
	}

	for _, file := range zr.File {
		if file.Name != "word/settings.xml" {
			continue
		}

		rc, openErr := file.Open()
		if openErr != nil {
			return &DOCXError{Code: ErrorCodeDocxInputProtected, Message: "unable to read word/settings.xml", Cause: openErr}
		}
		content, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return &DOCXError{Code: ErrorCodeDocxInputProtected, Message: "unable to inspect protection metadata", Cause: readErr}
		}

		text := strings.ToLower(string(content))
		if strings.Contains(text, "<w:documentprotection") || strings.Contains(text, "<w:writeprotection") {
			return &DOCXError{Code: ErrorCodeDocxInputProtected, Message: "protected docx files are not supported in v1"}
		}
		break
	}

	return nil
}

func mapProtectedFromExecution(logOutput string) error {
	normalized := strings.ToLower(logOutput)
	if strings.Contains(normalized, "password") || strings.Contains(normalized, "encrypted") || strings.Contains(normalized, "protection") {
		return &DOCXError{Code: ErrorCodeDocxInputProtected, Message: "protected docx files are not supported in v1"}
	}
	return nil
}

func detectFontIssue(logOutput string) error {
	normalized := strings.ToLower(logOutput)
	fontSignals := []string{
		"font",
		"substitut",
		"fallback",
		"not found",
		"missing",
	}

	hasFontSignal := false
	for _, signal := range fontSignals {
		if strings.Contains(normalized, signal) {
			hasFontSignal = true
			break
		}
	}

	if hasFontSignal {
		return &DOCXError{Code: ErrorCodeDocxFontSubstitution, Message: "font substitution/missing fonts detected during conversion"}
	}

	return nil
}

func moveFile(srcPath, dstPath string) error {
	if err := os.Rename(srcPath, dstPath); err == nil {
		return nil
	}

	input, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	if writeErr := os.WriteFile(dstPath, input, 0o644); writeErr != nil {
		return writeErr
	}

	if rmErr := os.Remove(srcPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return rmErr
	}

	return nil
}

func isFontDetectionError(err error) bool {
	if err == nil {
		return false
	}
	var docxErr *DOCXError
	if errors.As(err, &docxErr) {
		return docxErr.Code == ErrorCodeDocxFontSubstitution
	}
	return false
}
