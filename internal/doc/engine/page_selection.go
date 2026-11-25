package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"fileforge-desktop/internal/platform/fsx"
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
func ApplyPageSelection(inputPath, outputPath, selection string) error {
	return ApplyPageSelectionContext(context.Background(), inputPath, outputPath, selection)
}

// ApplyPageSelectionContext is ApplyPageSelection with cancellation.
//
// The replacement is atomic: the collected pages are written to a temporary
// file in the same directory and renamed over the output. On failure the
// temporary file and the output are removed, so a failed selection never
// leaves a partial document behind.
func ApplyPageSelectionContext(ctx context.Context, inputPath, outputPath, selection string) error {
	trimmed := NormalizePageSelection(selection)
	if trimmed == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	pageCount, countErr := api.PageCountFile(ctx, outputPath)
	if countErr != nil {
		_ = os.Remove(outputPath)
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      fmt.Errorf("read page count: %w", countErr),
		}
	}
	if rangeErr := validateSelectionForPageCount(trimmed, pageCount); rangeErr != nil {
		_ = os.Remove(outputPath)
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      rangeErr,
		}
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

	if collectErr := api.CollectFile(ctx, outputPath, tempPath, []string{trimmed}, model.NewDefaultConfiguration()); collectErr != nil {
		_ = os.Remove(outputPath)
		return &PageSelectionError{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Selection:  trimmed,
			Cause:      collectErr,
		}
	}

	if renameErr := fsx.ReplaceFile(tempPath, outputPath); renameErr != nil {
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

// validateSelectionForPageCount rejects selections that cannot select any page
// of the rendered document before pdfcpu extracts anything. pdfcpu clamps
// ranges, so a check performed only by CollectFile would accept a selection such
// as "9-12" on a two-page document and produce an empty result.
func validateSelectionForPageCount(selection string, pageCount int) error {
	if pageCount < 1 {
		return fmt.Errorf("document has no pages")
	}

	for _, token := range strings.Split(selection, ",") {
		value := strings.TrimSpace(token)
		if value == "" {
			continue
		}
		if value[0] == '!' || value[0] == 'n' {
			// Negated tokens never make a selection valid on their own.
			continue
		}

		switch {
		case value == "even":
			if pageCount >= 2 {
				return nil
			}
		case value == "odd":
			return nil
		case strings.HasPrefix(value, "l"):
			return nil
		case strings.HasPrefix(value, "-"):
			if number, err := strconv.Atoi(strings.TrimPrefix(value, "-")); err == nil && number >= 1 {
				return nil
			}
		case strings.HasSuffix(value, "-"):
			if number, err := strconv.Atoi(strings.TrimSuffix(value, "-")); err == nil && number >= 1 && number <= pageCount {
				return nil
			}
		default:
			parts := strings.Split(value, "-")
			if len(parts) >= 2 {
				if start, err := strconv.Atoi(parts[0]); err == nil && start >= 1 && start <= pageCount {
					return nil
				}
				continue
			}
			if page, err := strconv.Atoi(value); err == nil && page >= 1 && page <= pageCount {
				return nil
			}
		}
	}

	return fmt.Errorf("selection %q selects no page of a %d-page document", selection, pageCount)
}
