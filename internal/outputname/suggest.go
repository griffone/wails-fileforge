// Package outputname derives a better output base name from document metadata.
// It never renames anything: the caller decides whether to accept the
// suggestion, and collision resolution stays in outputplan.
package outputname

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	docengine "fileforge-desktop/internal/doc/engine"
	pdfengine "fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/platform/fsx"
)

// Suggestion sources, in resolution order per format.
const (
	SourcePDFTitle    = "pdf-title"
	SourceDOCXTitle   = "docx-title"
	SourceDOCXHeading = "docx-heading"
	SourceMDHeading   = "md-heading"
	SourceInputName   = "input-name"
)

// Detail codes for the only failures a suggestion can have: the path itself,
// reading the file, or decoding its metadata.
const (
	ErrorCodeInvalidPath  = "OUTPUT_NAME_SUGGESTION_INVALID_PATH"
	ErrorCodeReadFailed   = "OUTPUT_NAME_SUGGESTION_READ_FAILED"
	ErrorCodeDecodeFailed = "OUTPUT_NAME_SUGGESTION_DECODE_FAILED"
)

// Suggestion is the domain result. Found=false is a normal answer: nothing
// usable was available, and Reason explains why.
type Suggestion struct {
	Found         bool
	SuggestedName string
	Source        string
	Reason        string
}

// Error is a suggestion failure with a stable detail code.
type Error struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// Suggest resolves a base name for inputPath. toolID identifies the caller for
// error details; the format is decided by the input file itself so the same
// answer is returned regardless of which page asks.
//
// Resolution order:
//   - PDF: document title, then the cleaned input name.
//   - DOCX: core-properties title, first heading, then the cleaned input name.
//   - Markdown: first level-1 heading, then the cleaned input name.
//   - Anything else: the cleaned input name.
//
// No OCR runs here; metadata and document structure are the only sources.
func Suggest(toolID, inputPath string) (Suggestion, *Error) {
	path := strings.TrimSpace(inputPath)
	if path == "" {
		return Suggestion{}, newError(ErrorCodeInvalidPath, "inputPath is required", toolID, path, nil)
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return Suggestion{}, newError(
				ErrorCodeInvalidPath,
				fmt.Sprintf("input file was not found: %s", filepath.Base(path)),
				toolID,
				path,
				statErr,
			)
		}

		return Suggestion{}, newError(
			ErrorCodeReadFailed,
			fmt.Sprintf("input file is not accessible: %s", filepath.Base(path)),
			toolID,
			path,
			statErr,
		)
	}
	if info.IsDir() {
		return Suggestion{}, newError(
			ErrorCodeInvalidPath,
			fmt.Sprintf("input path is a directory: %s", filepath.Base(path)),
			toolID,
			path,
			nil,
		)
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return suggestPDF(toolID, path)
	case ".docx":
		return suggestDOCX(toolID, path, info.Size())
	case ".md", ".markdown":
		return suggestMarkdown(toolID, path)
	default:
		return inputNameSuggestion(toolID, path), nil
	}
}

func suggestPDF(toolID, path string) (Suggestion, *Error) {
	file, openErr := os.Open(path)
	if openErr != nil {
		return Suggestion{}, readError(toolID, path, openErr)
	}
	defer func() { _ = file.Close() }()

	title, titleErr := pdfengine.ReadDocumentTitle(context.Background(), file, filepath.Base(path))
	if titleErr != nil {
		return Suggestion{}, decodeError(toolID, path, titleErr)
	}

	if suggestion, ok := metadataSuggestion(title, SourcePDFTitle); ok {
		return suggestion, nil
	}

	return inputNameSuggestion(toolID, path), nil
}

func suggestDOCX(toolID, path string, size int64) (Suggestion, *Error) {
	file, openErr := os.Open(path)
	if openErr != nil {
		return Suggestion{}, readError(toolID, path, openErr)
	}
	defer func() { _ = file.Close() }()

	titles, titlesErr := docengine.ReadDOCXTitles(file, size)
	if titlesErr != nil {
		return Suggestion{}, decodeError(toolID, path, titlesErr)
	}

	if suggestion, ok := metadataSuggestion(titles.CoreTitle, SourceDOCXTitle); ok {
		return suggestion, nil
	}
	if suggestion, ok := metadataSuggestion(titles.Heading, SourceDOCXHeading); ok {
		return suggestion, nil
	}

	return inputNameSuggestion(toolID, path), nil
}

func suggestMarkdown(toolID, path string) (Suggestion, *Error) {
	source, readErr := os.ReadFile(path)
	if readErr != nil {
		return Suggestion{}, readError(toolID, path, readErr)
	}

	heading, headingErr := docengine.ReadMarkdownTitle(source)
	if headingErr != nil {
		return Suggestion{}, decodeError(toolID, path, headingErr)
	}

	if suggestion, ok := metadataSuggestion(heading, SourceMDHeading); ok {
		return suggestion, nil
	}

	return inputNameSuggestion(toolID, path), nil
}

// metadataSuggestion sanitizes a metadata value and reports whether it is
// usable. A value that sanitizes to nothing falls through to the next source.
func metadataSuggestion(raw, source string) (Suggestion, bool) {
	name := fsx.SanitizeBaseName(raw)
	if name == "" {
		return Suggestion{}, false
	}

	return Suggestion{
		Found:         true,
		SuggestedName: name,
		Source:        source,
	}, true
}

// inputNameSuggestion cleans the input base name: extension removed, separator
// runs collapsed, then the shared filesystem sanitizer applied.
func inputNameSuggestion(toolID, path string) Suggestion {
	name := fsx.SanitizeBaseName(cleanedInputStem(path))
	if name == "" {
		return Suggestion{
			Found:  false,
			Reason: "the input file name has no usable characters",
		}
	}

	return Suggestion{
		Found:         true,
		SuggestedName: name,
		Source:        SourceInputName,
		Reason:        "no document title or heading was found",
	}
}

func cleanedInputStem(path string) string {
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.TrimSpace(stem) == "" {
		stem = base
	}

	replacer := strings.NewReplacer("_", " ", "-", " ")
	stem = replacer.Replace(stem)

	return strings.TrimSpace(stem)
}

func newError(code, message, toolID, inputPath string, cause error) *Error {
	details := map[string]any{}
	if toolID != "" {
		details["toolId"] = toolID
	}
	if inputPath != "" {
		details["inputPath"] = inputPath
	}
	if len(details) == 0 {
		details = nil
	}

	return &Error{Code: code, Message: message, Details: details, Cause: cause}
}

func readError(toolID, path string, cause error) *Error {
	return newError(
		ErrorCodeReadFailed,
		fmt.Sprintf("cannot read %s", filepath.Base(path)),
		toolID,
		path,
		cause,
	)
}

func decodeError(toolID, path string, cause error) *Error {
	return newError(
		ErrorCodeDecodeFailed,
		fmt.Sprintf("cannot read document metadata from %s", filepath.Base(path)),
		toolID,
		path,
		cause,
	)
}
