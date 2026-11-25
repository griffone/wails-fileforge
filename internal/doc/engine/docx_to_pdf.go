package engine

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bobyeoh/docx2pdf-go"

	"fileforge-desktop/internal/platform/execx"
	"fileforge-desktop/internal/platform/fsx"
)

const (
	// EngineDocx2PDF names the bundled pure-Go renderer.
	EngineDocx2PDF = "docx2pdf"

	ErrorCodeDocxInputProtected   = "DOC_DOCX_TO_PDF_INPUT_PROTECTED"
	ErrorCodeDocxInputEncrypted   = "DOC_DOCX_TO_PDF_INPUT_ENCRYPTED"
	ErrorCodeDocxFontSubstitution = "DOC_DOCX_TO_PDF_FONTS_MISSING_OR_SUBSTITUTED"
	ErrorCodeDocxPrimaryExecution = "DOC_DOCX_TO_PDF_PRIMARY_EXECUTION_FAILED"
	ErrorCodeDocxPDFAUnsupported  = "DOC_DOCX_TO_PDF_PDFA_UNSUPPORTED"
	// ErrorCodeDocxPDFAGhostscriptMissing reports the only external runtime the
	// tool still needs: Ghostscript, and only for PDF/A post-processing.
	ErrorCodeDocxPDFAGhostscriptMissing = "DOC_DOCX_TO_PDF_RUNTIME_GHOSTSCRIPT_MISSING"
	ErrorCodeDocxPageSelectionInvalid   = "DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID"

	// Deprecated compatibility codes. The frontend error vocabulary recognizes
	// these strings and older jobs may still carry them, so they stay declared
	// even though the pure-Go engine never emits them.
	ErrorCodeDocxRuntimePrimaryMissing  = "DOC_DOCX_TO_PDF_RUNTIME_LIBREOFFICE_MISSING"
	ErrorCodeDocxRuntimeFallbackMissing = "DOC_DOCX_TO_PDF_RUNTIME_PANDOC_MISSING"
	ErrorCodeDocxFallbackExecution      = "DOC_DOCX_TO_PDF_FALLBACK_EXECUTION_FAILED"
)

// cfbMagic is the OLE Compound File signature Word uses for password-protected
// DOCX packages. Those files are not ZIP archives, so the library cannot open
// them.
var cfbMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// CommandResult holds the bounded stderr tail of an external command.
type CommandResult struct {
	Stderr string
}

// CommandRunner runs the external process used for PDF/A post-processing.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (CommandResult, error)
}

// DOCXRenderer renders one DOCX file to a PDF at outputPath. The production
// implementation wraps the pure-Go docx2pdf library; tests inject a stub and a
// future engine swap stays local to this package.
type DOCXRenderer interface {
	Render(ctx context.Context, inputPath, outputPath string) error
}

// LibraryRenderer renders through github.com/bobyeoh/docx2pdf-go. Empty
// Options let the library resolve a system font (Arial/Helvetica on
// Windows/macOS, DejaVu/Liberation/Noto on Linux) with an embedded Latin
// fallback.
type LibraryRenderer struct{}

func (r *LibraryRenderer) Render(ctx context.Context, inputPath, outputPath string) error {
	return docx2pdf.ConvertContext(ctx, inputPath, outputPath, docx2pdf.Options{})
}

// ExecCommandRunner runs a command through execx so cancellation kills the
// whole process group and stderr stays bounded.
type ExecCommandRunner struct{}

func (r *ExecCommandRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	result, err := execx.Run(ctx, execx.Command{Path: name, Args: args})

	return CommandResult{Stderr: result.Stderr}, err
}

type ConvertDOCXRequest struct {
	InputPath  string
	OutputPath string
	// PDFA requests a PDF/A-2 export. The document renders normally first and
	// Ghostscript post-processes the result; a missing Ghostscript is an error,
	// never a silent downgrade.
	PDFA bool
	// PageSelection keeps only the requested pages of the rendered PDF. The
	// range is validated against the real page count.
	PageSelection string
	// SkipProtectedCheck skips the package protection scan. Callers that
	// already ran DetectProtectedDOCX during planning set it to avoid scanning
	// twice.
	SkipProtectedCheck bool
}

type ConvertDOCXResult struct {
	EngineUsed string
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

// ConvertDOCX renders a DOCX file and applies the optional PDF/A and page
// selection steps. Every step writes a sibling temporary file and publishes the
// final result with fsx.ReplaceFile, so a failure never leaves a partial output
// at the destination.
func ConvertDOCX(ctx context.Context, renderer DOCXRenderer, runner CommandRunner, req ConvertDOCXRequest) (ConvertDOCXResult, error) {
	result := ConvertDOCXResult{EngineUsed: EngineDocx2PDF}

	if renderer == nil {
		renderer = &LibraryRenderer{}
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}

	if !req.SkipProtectedCheck {
		if err := DetectProtectedDOCX(req.InputPath); err != nil {
			return result, err
		}
	}

	if err := fsx.EnsureDir(filepath.Dir(req.OutputPath), 0o755); err != nil {
		return result, &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "unable to create output directory", Cause: err}
	}

	workPath, err := createSiblingTemp(req.OutputPath, ".pdf")
	if err != nil {
		return result, &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "unable to prepare the output file", Cause: err}
	}
	// The renderer creates the file itself; remove the placeholder first so the
	// library always writes a fresh document.
	_ = os.Remove(workPath)

	published := false
	defer func() {
		if !published {
			_ = os.Remove(workPath)
		}
	}()

	if err := renderer.Render(ctx, req.InputPath, workPath); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return result, mapRenderError(err)
	}

	if _, statErr := os.Stat(workPath); statErr != nil {
		return result, &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "the renderer did not produce an output PDF", Cause: statErr}
	}

	if req.PDFA {
		pdfaPath, tempErr := createSiblingTemp(req.OutputPath, ".pdfa.pdf")
		if tempErr != nil {
			return result, &DOCXError{Code: ErrorCodeDocxPDFAUnsupported, Message: "unable to prepare the PDF/A output file", Cause: tempErr}
		}
		if pdfaErr := applyPDFA(ctx, runner, workPath, pdfaPath); pdfaErr != nil {
			_ = os.Remove(pdfaPath)
			return result, pdfaErr
		}
		// The PDF/A copy is now the working document.
		_ = os.Remove(workPath)
		workPath = pdfaPath
	}

	if selection := strings.TrimSpace(req.PageSelection); selection != "" {
		if selectionErr := ApplyPageSelectionContext(ctx, req.InputPath, workPath, selection); selectionErr != nil {
			return result, mapPageSelectionError(req.InputPath, selection, selectionErr)
		}
	}

	if err := fsx.ReplaceFile(workPath, req.OutputPath); err != nil {
		return result, &DOCXError{Code: ErrorCodeDocxPrimaryExecution, Message: "unable to finalize the converted PDF", Cause: err}
	}
	published = true

	return result, nil
}

// applyPDFA distills a rendered PDF into PDF/A-2 with Ghostscript. Missing gs is
// reported as a dependency error with install guidance; every other failure is
// an explicit PDF/A error. Neither path falls back to a plain PDF.
func applyPDFA(ctx context.Context, runner CommandRunner, inputPath, outputPath string) error {
	args := []string{
		"-q",
		"-dPDFA=2",
		"-dBATCH",
		"-dNOPAUSE",
		"-sDEVICE=pdfwrite",
		"-dPDFACompatibilityPolicy=1",
		"-sOutputFile=" + outputPath,
		inputPath,
	}

	result, err := runner.Run(ctx, "gs", args...)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, exec.ErrNotFound) {
			return &DOCXError{
				Code:    ErrorCodeDocxPDFAGhostscriptMissing,
				Message: "Ghostscript (gs) is required for PDF/A export but is not installed. Install Ghostscript or turn off the archival option.",
				Cause:   err,
				Details: map[string]any{"runtime": "gs"},
			}
		}
		return &DOCXError{
			Code:    ErrorCodeDocxPDFAUnsupported,
			Message: "Ghostscript could not produce the requested PDF/A file",
			Cause:   err,
			Details: map[string]any{"stderr": strings.TrimSpace(result.Stderr)},
		}
	}

	if info, statErr := os.Stat(outputPath); statErr != nil || info.Size() == 0 {
		if statErr == nil {
			statErr = errors.New("ghostscript produced an empty file")
		}
		return &DOCXError{
			Code:    ErrorCodeDocxPDFAUnsupported,
			Message: "Ghostscript did not produce a PDF/A file",
			Cause:   statErr,
			Details: map[string]any{"stderr": strings.TrimSpace(result.Stderr)},
		}
	}

	return nil
}

// mapRenderError keeps font failures distinguishable from generic render
// failures so the UI can give the right guidance.
func mapRenderError(err error) error {
	if err == nil {
		return nil
	}

	message := strings.ToLower(err.Error())
	if strings.Contains(message, "font") {
		return &DOCXError{
			Code:    ErrorCodeDocxFontSubstitution,
			Message: "unable to resolve a usable font for the DOCX rendering",
			Cause:   err,
		}
	}

	return &DOCXError{
		Code:    ErrorCodeDocxPrimaryExecution,
		Message: "the DOCX renderer could not convert the document",
		Cause:   err,
	}
}

func mapPageSelectionError(inputPath, selection string, err error) error {
	return &DOCXError{
		Code:    ErrorCodeDocxPageSelectionInvalid,
		Message: fmt.Sprintf("page selection %q cannot be applied to %s. The document may have fewer pages than the selection.", selection, filepath.Base(inputPath)),
		Cause:   err,
		Details: map[string]any{"inputPath": inputPath, "pageSelection": selection},
	}
}

// DetectProtectedDOCX rejects password-protected or encrypted packages before
// they reach the renderer. It recognizes the OLE/CFB signature Word uses for
// encrypted DOCX files, the ZIP encryption flag, and document protection
// metadata.
func DetectProtectedDOCX(inputPath string) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "unable to inspect docx protection metadata", Cause: err}
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "unable to inspect docx file metadata", Cause: err}
	}

	header := make([]byte, len(cfbMagic))
	if _, readErr := io.ReadFull(file, header); readErr == nil && string(header) == string(cfbMagic) {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "encrypted or password-protected docx files are not supported"}
	}
	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "unable to inspect docx protection metadata", Cause: seekErr}
	}

	archive, err := zip.NewReader(file, info.Size())
	if err != nil {
		return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "docx appears encrypted or is not a valid package", Cause: err}
	}

	for _, entry := range archive.File {
		if entry.Flags&0x1 != 0 {
			return &DOCXError{Code: ErrorCodeDocxInputEncrypted, Message: "encrypted docx entries are not supported"}
		}
	}

	for _, entry := range archive.File {
		if entry.Name != "word/settings.xml" {
			continue
		}

		reader, openErr := entry.Open()
		if openErr != nil {
			return &DOCXError{Code: ErrorCodeDocxInputProtected, Message: "unable to read word/settings.xml", Cause: openErr}
		}
		content, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			return &DOCXError{Code: ErrorCodeDocxInputProtected, Message: "unable to inspect protection metadata", Cause: readErr}
		}

		text := strings.ToLower(string(content))
		if strings.Contains(text, "<w:documentprotection") || strings.Contains(text, "<w:writeprotection") {
			return &DOCXError{Code: ErrorCodeDocxInputProtected, Message: "protected docx files are not supported"}
		}
		break
	}

	return nil
}
