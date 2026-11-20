package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// pageSelectionTokenRegexp mirrors pdfcpu's intended page selection grammar.
// pdfcpu's ParsePageSelection anchors each alternative separately, so malformed
// expressions like "1,,3" or "1-2-3" slip through. The strict token check below
// closes that gap before the selection reaches CollectFile.
var pageSelectionTokenRegexp = regexp.MustCompile(`^(even|odd|[!n]?((-\d+)|(\d+(-(\d+)?)?)|((\d+)?-l(-\d+)?)|(l(-(\d+)-?)?)))$`)

// PageSelectionError reports that a page selection could not be applied to a
// converted PDF. The tool layer maps it to a canonical job error.
type PageSelectionError struct {
	InputPath  string
	OutputPath string
	Selection  string
	Cause      error
}

func (e *PageSelectionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("apply page selection %q to %s", e.Selection, filepath.Base(e.InputPath))
	}
	return fmt.Sprintf("apply page selection %q to %s: %v", e.Selection, filepath.Base(e.InputPath), e.Cause)
}

func (e *PageSelectionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NormalizePageSelection removes whitespace around the comma separated tokens
// of a page selection so "1-3, 5" becomes "1-3,5".
func NormalizePageSelection(selection string) string {
	trimmed := strings.TrimSpace(selection)
	if trimmed == "" {
		return ""
	}

	parts := strings.Split(trimmed, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	return strings.Join(parts, ",")
}

// ValidatePageSelection checks pdfcpu page selection syntax ("1-3,5"). An empty
// selection means "all pages" and is valid.
func ValidatePageSelection(selection string) error {
	trimmed := NormalizePageSelection(selection)
	if trimmed == "" {
		return nil
	}

	tokens, err := api.ParsePageSelection(trimmed)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return fmt.Errorf("page selection is empty")
	}

	for _, token := range tokens {
		if !pageSelectionTokenRegexp.MatchString(token) {
			return fmt.Errorf("invalid page selection token %q", token)
		}
	}

	return nil
}

// ApplyPageSelection replaces outputPath with a PDF that only contains the
// selected pages. An empty selection is a no-op.
//
// The replacement is atomic: the collected pages are written to a temporary
// file in the same directory and renamed over the output. On failure the
// temporary file and the output are removed, so a failed selection never
// leaves a partial document behind.
func ApplyPageSelection(inputPath, outputPath, selection string) error {
	trimmed := NormalizePageSelection(selection)
	if trimmed == "" {
		return nil
	}

	tempFile, err := os.CreateTemp(filepath.Dir(outputPath), ".fileforge-pages-*.pdf")
	if err != nil {
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      fmt.Errorf("prepare temporary file: %w", err),
		}
	}

	tempPath := tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      fmt.Errorf("prepare temporary file: %w", closeErr),
		}
	}
	// CreateTemp uses 0600; align the final document with regular conversion
	// outputs before it replaces the engine result.
	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		_ = os.Remove(tempPath)
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      fmt.Errorf("prepare temporary file: %w", chmodErr),
		}
	}
	defer func() { _ = os.Remove(tempPath) }()

	if collectErr := api.CollectFile(context.Background(), outputPath, tempPath, []string{trimmed}, model.NewDefaultConfiguration()); collectErr != nil {
		_ = os.Remove(outputPath)
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      collectErr,
		}
	}

	if renameErr := os.Rename(tempPath, outputPath); renameErr != nil {
		_ = os.Remove(outputPath)
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      renameErr,
		}
	}

	return nil
}
