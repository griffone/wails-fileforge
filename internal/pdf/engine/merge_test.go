package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestMergeSuccessWithMultiplePDFs(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.pdf")
	inputB := filepath.Join(tmpDir, "b.pdf")
	inputC := filepath.Join(tmpDir, "c.pdf")
	out := filepath.Join(tmpDir, "merged.pdf")

	writeMinimalPDF(t, inputA, "A")
	writeMinimalPDF(t, inputB, "B")
	writeMinimalPDF(t, inputC, "C")

	err := Merge(context.Background(), []string{inputA, inputB, inputC}, out)
	if err != nil {
		t.Fatalf("expected merge success, got error: %v", err)
	}

	if validateErr := api.ValidateFile(out, nil); validateErr != nil {
		t.Fatalf("expected valid merged PDF, got: %v", validateErr)
	}
}

func TestMergeValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()
	validA := filepath.Join(tmpDir, "a.pdf")
	validB := filepath.Join(tmpDir, "b.pdf")
	writeMinimalPDF(t, validA, "A")
	writeMinimalPDF(t, validB, "B")

	tests := []struct {
		name   string
		inputs []string
		out    string
	}{
		{
			name:   "less than 2 inputs",
			inputs: []string{validA},
			out:    filepath.Join(tmpDir, "out.pdf"),
		},
		{
			name:   "missing output path",
			inputs: []string{validA, validB},
			out:    "",
		},
		{
			name:   "input extension is not pdf",
			inputs: []string{filepath.Join(tmpDir, "note.txt"), validB},
			out:    filepath.Join(tmpDir, "out.pdf"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := Merge(context.Background(), tt.inputs, tt.out)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			var mergeErr *MergeError
			if !errors.As(err, &mergeErr) {
				t.Fatalf("expected MergeError, got %T", err)
			}

			if mergeErr.Code != ErrorCodeValidation {
				t.Fatalf("expected code %s, got %s", ErrorCodeValidation, mergeErr.Code)
			}
		})
	}
}

func TestMergeRejectsDuplicateInputsCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	validA := filepath.Join(tmpDir, "a.pdf")
	writeMinimalPDF(t, validA, "A")

	err := Merge(context.Background(), []string{validA, strings.ToUpper(validA)}, filepath.Join(tmpDir, "out.pdf"))
	if err == nil {
		t.Fatal("expected duplicate input validation error")
	}

	var mergeErr *MergeError
	if !errors.As(err, &mergeErr) {
		t.Fatalf("expected MergeError, got %T", err)
	}
	if mergeErr.Code != ErrorCodeDuplicateInput {
		t.Fatalf("expected code %s, got %s", ErrorCodeDuplicateInput, mergeErr.Code)
	}
}

func TestMergeRejectsOutputCollisionWithInput(t *testing.T) {
	tmpDir := t.TempDir()
	validA := filepath.Join(tmpDir, "a.pdf")
	validB := filepath.Join(tmpDir, "b.pdf")
	writeMinimalPDF(t, validA, "A")
	writeMinimalPDF(t, validB, "B")

	err := Merge(context.Background(), []string{validA, validB}, validA)
	if err == nil {
		t.Fatal("expected output collision validation error")
	}

	var mergeErr *MergeError
	if !errors.As(err, &mergeErr) {
		t.Fatalf("expected MergeError, got %T", err)
	}
	if mergeErr.Code != ErrorCodeOutputCollidesInput {
		t.Fatalf("expected code %s, got %s", ErrorCodeOutputCollidesInput, mergeErr.Code)
	}
}

func TestNormalizePathKeyCrossPlatformMatrix(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		eq   bool
	}{
		{
			name: "posix case insensitive and dot segments",
			a:    "/tmp/A/../docs/input.pdf",
			b:    "/tmp/docs/INPUT.pdf",
			eq:   true,
		},
		{
			name: "windows drive style with slash variants",
			a:    `C:\Users\Ivan\Docs\a.pdf`,
			b:    `c:/users/ivan/docs/A.PDF`,
			eq:   true,
		},
		{
			name: "windows drive with relative segments",
			a:    `D:\merge\in\..\final\doc.pdf`,
			b:    `d:/merge/final/DOC.PDF`,
			eq:   true,
		},
		{
			name: "unc style path normalization",
			a:    `\\SERVER\Share\Docs\a.pdf`,
			b:    `//server/share/docs/A.PDF`,
			eq:   true,
		},
		{
			name: "different roots are not equal",
			a:    `C:\docs\a.pdf`,
			b:    `D:\docs\a.pdf`,
			eq:   false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			left := normalizePathKey(tt.a)
			right := normalizePathKey(tt.b)
			if (left == right) != tt.eq {
				t.Fatalf("normalizePathKey equality mismatch: %q => %q, %q => %q, eq=%v", tt.a, left, tt.b, right, tt.eq)
			}
		})
	}
}

func TestMergeOutputDirectoryActionableErrors(t *testing.T) {
	tmpDir := t.TempDir()
	inputA := filepath.Join(tmpDir, "a.pdf")
	inputB := filepath.Join(tmpDir, "b.pdf")
	writeMinimalPDF(t, inputA, "A")
	writeMinimalPDF(t, inputB, "B")

	t.Run("missing directory", func(t *testing.T) {
		missingOut := filepath.Join(tmpDir, "missing-dir", "merged.pdf")
		err := Merge(context.Background(), []string{inputA, inputB}, missingOut)
		assertMergeCode(t, err, ErrorCodeOutputDirNotFound)
	})

	t.Run("parent is file", func(t *testing.T) {
		parentFile := filepath.Join(tmpDir, "parent-file")
		if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
			t.Fatalf("write parent file: %v", err)
		}
		notDirOut := filepath.Join(parentFile, "merged.pdf")
		err := Merge(context.Background(), []string{inputA, inputB}, notDirOut)
		assertMergeCode(t, err, ErrorCodeOutputDirNotDirectory)
	})

	if runtime.GOOS != "windows" {
		t.Run("not writable directory", func(t *testing.T) {
			lockedDir := filepath.Join(tmpDir, "locked")
			if err := os.MkdirAll(lockedDir, 0o755); err != nil {
				t.Fatalf("mkdir locked dir: %v", err)
			}
			if err := os.Chmod(lockedDir, 0o555); err != nil {
				t.Fatalf("chmod locked dir: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })

			err := Merge(context.Background(), []string{inputA, inputB}, filepath.Join(lockedDir, "merged.pdf"))
			if err == nil {
				t.Skip("filesystem permissions allow write despite chmod; skipping not-writable assertion")
			}
			assertMergeCode(t, err, ErrorCodeOutputDirNotWritable)
		})
	}
}

func assertMergeCode(t *testing.T, err error, expectedCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected merge error with code %s, got nil", expectedCode)
	}

	var mergeErr *MergeError
	if !errors.As(err, &mergeErr) {
		t.Fatalf("expected MergeError, got %T", err)
	}

	if mergeErr.Code != expectedCode {
		t.Fatalf("expected code %s, got %s", expectedCode, mergeErr.Code)
	}
}

func TestMergeRejectsCorruptPDFInput(t *testing.T) {
	tmpDir := t.TempDir()
	validPDF := filepath.Join(tmpDir, "valid.pdf")
	corruptPDF := filepath.Join(tmpDir, "corrupt.pdf")
	out := filepath.Join(tmpDir, "merged.pdf")

	writeMinimalPDF(t, validPDF, "Valid")
	if err := os.WriteFile(corruptPDF, []byte("not a real pdf"), 0o644); err != nil {
		t.Fatalf("failed writing corrupt file: %v", err)
	}

	err := Merge(context.Background(), []string{validPDF, corruptPDF}, out)
	if err == nil {
		t.Fatal("expected merge error for corrupt PDF, got nil")
	}

	var mergeErr *MergeError
	if !errors.As(err, &mergeErr) {
		t.Fatalf("expected MergeError, got %T", err)
	}

	if mergeErr.Code != ErrorCodeInvalidInputPDF {
		t.Fatalf("expected code %s, got %s", ErrorCodeInvalidInputPDF, mergeErr.Code)
	}
}

func writeMinimalPDF(t *testing.T, path, label string) {
	t.Helper()

	content := fmt.Sprintf("BT /F1 24 Tf 40 100 Td (%s) Tj ET", label)
	b := &bytes.Buffer{}
	b.WriteString("%PDF-1.4\n")

	offsets := make([]int, 6)

	writeObj := func(objID int, body string) {
		offsets[objID] = b.Len()
		_, _ = fmt.Fprintf(b, "%d 0 obj\n%s\nendobj\n", objID, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>")
	writeObj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	writeObj(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xrefOffset := b.Len()
	b.WriteString("xref\n0 6\n")
	b.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		_, _ = fmt.Fprintf(b, "%010d 00000 n \n", offsets[i])
	}
	b.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	_, _ = fmt.Fprintf(b, "startxref\n%d\n%%%%EOF\n", xrefOffset)

	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatalf("failed writing minimal pdf: %v", err)
	}
}
