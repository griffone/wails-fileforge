package jobs

import (
	"testing"

	"fileforge-desktop/internal/models"
)

func TestIsRetryableErrorMatchesEmittedDetailCodes(t *testing.T) {
	tests := []struct {
		name       string
		toolID     string
		detailCode string
		want       bool
	}{
		{name: "image convert single", toolID: "tool.image.convert", detailCode: "IMAGE_SINGLE_EXECUTION", want: true},
		{name: "image convert batch", toolID: "tool.image.convert", detailCode: "IMAGE_BATCH_ITEM", want: true},
		{name: "image crop execution", toolID: "tool.image.crop", detailCode: "IMAGE_CROP_EXECUTION", want: true},
		{name: "image crop batch", toolID: "tool.image.crop", detailCode: "IMAGE_CROP_BATCH_ITEM", want: true},
		{name: "image annotate execution", toolID: "tool.image.annotate", detailCode: "IMAGE_ANNOTATE_EXECUTION", want: true},
		{name: "image annotate batch", toolID: "tool.image.annotate", detailCode: "IMAGE_ANNOTATE_BATCH_ITEM", want: true},
		{name: "pdf merge", toolID: "tool.pdf.merge", detailCode: "PDF_MERGE_FAILED", want: true},
		{name: "pdf split", toolID: "tool.pdf.split", detailCode: "PDF_SPLIT_FAILED", want: true},
		{name: "pdf crop", toolID: "tool.pdf.crop", detailCode: "PDF_CROP_FAILED", want: true},
		{name: "docx primary", toolID: "tool.doc.docx_to_pdf", detailCode: "DOC_DOCX_TO_PDF_PRIMARY_EXECUTION_FAILED", want: true},
		{name: "docx execution", toolID: "tool.doc.docx_to_pdf", detailCode: "DOC_DOCX_TO_PDF_EXECUTION_FAILED", want: true},
		{name: "markdown render", toolID: "tool.doc.md_to_pdf", detailCode: "DOC_MD_TO_PDF_RENDER_FAILED", want: true},
		{name: "video convert", toolID: "tool.video.convert", detailCode: "VIDEO_CONVERT_EXECUTION_FAILED", want: true},
		{name: "video trim", toolID: "tool.video.trim", detailCode: "VIDEO_TRIM_EXECUTION_FAILED", want: true},
		{name: "video merge", toolID: "tool.video.merge", detailCode: "VIDEO_MERGE_EXECUTION_FAILED", want: true},
		{name: "image crop validation", toolID: "tool.image.crop", detailCode: "IMAGE_CROP_DIMENSIONS_INVALID", want: false},
		{name: "pdf split validation", toolID: "tool.pdf.split", detailCode: "PDF_SPLIT_RANGES_INVALID", want: false},
		{name: "docx runtime missing", toolID: "tool.doc.docx_to_pdf", detailCode: "DOC_DOCX_TO_PDF_RUNTIME_GHOSTSCRIPT_MISSING", want: false},
		{name: "video runtime missing", toolID: "tool.video.convert", detailCode: "VIDEO_RUNTIME_UNAVAILABLE", want: false},
		{name: "unknown tool", toolID: "tool.unknown", detailCode: "IMAGE_CROP_EXECUTION", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobErr := &models.JobError{Code: "EXEC_IO_TRANSIENT", DetailCode: tt.detailCode}
			if got := isRetryableError(tt.toolID, jobErr); got != tt.want {
				t.Fatalf("isRetryableError(%q, %q) = %v, want %v", tt.toolID, tt.detailCode, got, tt.want)
			}
		})
	}
}

func TestIsRetryableErrorFallsBackToCode(t *testing.T) {
	jobErr := &models.JobError{Code: "VIDEO_CONVERT_EXECUTION_FAILED"}
	if !isRetryableError("tool.video.convert", jobErr) {
		t.Fatal("expected retryable when only code is set")
	}

	if isRetryableError("tool.video.convert", nil) {
		t.Fatal("expected nil error to be non-retryable")
	}
}
