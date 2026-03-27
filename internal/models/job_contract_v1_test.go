package models

import "testing"

func TestCanonicalErrorCodeMapping(t *testing.T) {
	tests := []struct {
		name       string
		detailCode string
		want       string
	}{
		{name: "validation detail", detailCode: "PDF_INVALID_INPUT", want: ErrorCodeValidationInvalidInput},
		{name: "runtime detail", detailCode: "VIDEO_RUNTIME_UNAVAILABLE", want: ErrorCodeRuntimeDepMissing},
		{name: "timeout detail", detailCode: "EXEC_TIMEOUT", want: ErrorCodeExecTimeoutTransient},
		{name: "unsupported detail", detailCode: "FORMAT_MISMATCH", want: ErrorCodeUnsupportedFormat},
		{name: "cancelled detail", detailCode: "JOB_CANCELLED", want: ErrorCodeCancelledByUser},
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
