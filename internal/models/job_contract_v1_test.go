package models

import "testing"

func TestCanonicalErrorCodeMapping(t *testing.T) {
	tests := []struct {
		name       string
		detailCode string
		want       string
	}{
		{name: "validation detail", detailCode: "PDF_INVALID_INPUT", want: ErrorCodeValidationInvalidInput},
		{name: "protected detail", detailCode: "PDF_PROTECTED_INPUT", want: ErrorCodeValidationInvalidInput},
		{name: "runtime detail", detailCode: "VIDEO_RUNTIME_UNAVAILABLE", want: ErrorCodeRuntimeDepMissing},
		{name: "timeout detail", detailCode: "EXEC_TIMEOUT", want: ErrorCodeExecTimeoutTransient},
		{name: "unsupported detail", detailCode: "FORMAT_MISMATCH", want: ErrorCodeUnsupportedFormat},
		{name: "cancelled detail", detailCode: "JOB_CANCELLED", want: ErrorCodeCancelledByUser},
		{name: "doc validation detail", detailCode: "DOC_MD_TO_PDF_PLACEHOLDER_INVALID", want: ErrorCodeValidationInvalidInput},
		{name: "doc render detail", detailCode: "DOC_MD_TO_PDF_RENDER_FAILED", want: ErrorCodeExecIOTransient},
		{name: "docx runtime detail", detailCode: "DOC_DOCX_TO_PDF_RUNTIME_LIBREOFFICE_MISSING", want: ErrorCodeRuntimeDepMissing},
		{name: "docx input detail", detailCode: "DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED", want: ErrorCodeValidationInvalidInput},
		{name: "docx execution detail", detailCode: "DOC_DOCX_TO_PDF_FALLBACK_EXECUTION_FAILED", want: ErrorCodeExecIOTransient},
		{name: "default detail", detailCode: "EXECUTE_FAILED", want: ErrorCodeExecIOTransient},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CanonicalErrorCode(tc.detailCode)
			if got != tc.want {
				t.Fatalf("CanonicalErrorCode(%q) = %q, want %q", tc.detailCode, got, tc.want)
			}
		})
	}
}

func TestNewCanonicalJobErrorSetsDetailCode(t *testing.T) {
	err := NewCanonicalJobError("PDF_CROP_FAILED", "crop failed", map[string]any{"file": "a.pdf"})

	if err == nil {
		t.Fatal("expected non-nil error")
	}

	if err.Code != ErrorCodeExecIOTransient {
		t.Fatalf("expected canonical code %s, got %s", ErrorCodeExecIOTransient, err.Code)
	}

	if err.DetailCode != "PDF_CROP_FAILED" {
		t.Fatalf("expected detail code PDF_CROP_FAILED, got %s", err.DetailCode)
	}
}
