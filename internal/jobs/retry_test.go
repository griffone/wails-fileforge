package jobs

import (
	"testing"

	"fileforge-desktop/internal/models"
)

// registeredToolIDs is every tool the app registers. Cancellation and unknown
// code tests run against all of them.
var registeredToolIDs = []string{
	"tool.image.convert",
	"tool.image.crop",
	"tool.image.annotate",
	"tool.image.compress",
	"tool.image.ocr",
	"tool.image.background",
	"tool.image.watermark",
	"tool.pdf.merge",
	"tool.pdf.split",
	"tool.pdf.crop",
	"tool.pdf.optimize",
	"tool.pdf.ocr",
	"tool.pdf.images",
	"tool.pdf.to_docx",
	"tool.pdf.rotate",
	"tool.pdf.remove_pages",
	"tool.pdf.reorder",
	"tool.pdf.extract_images",
	"tool.doc.docx_to_pdf",
	"tool.doc.md_to_pdf",
	"tool.video.convert",
	"tool.video.trim",
	"tool.video.merge",
	"tool.video.transcribe",
	"tool.audio.extract",
	"tool.audio.convert",
	"tool.audio.normalize",
}

func TestIsRetryableErrorMatchesEmittedDetailCodes(t *testing.T) {
	tests := []struct {
		name       string
		toolID     string
		detailCode string
		want       bool
	}{
		// Existing allowlist.
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

		// image.compress: read/encode/write failures.
		{name: "image compress single execution", toolID: "tool.image.compress", detailCode: "IMAGE_COMPRESS_EXECUTION", want: true},
		{name: "image compress batch item", toolID: "tool.image.compress", detailCode: "IMAGE_COMPRESS_BATCH_ITEM", want: true},

		// image.ocr: Tesseract process and output I/O.
		{name: "image ocr single execution", toolID: "tool.image.ocr", detailCode: "IMAGE_OCR_EXECUTION", want: true},
		{name: "image ocr batch item", toolID: "tool.image.ocr", detailCode: "IMAGE_OCR_BATCH_ITEM", want: true},
		{name: "image ocr normalize", toolID: "tool.image.ocr", detailCode: "IMAGE_OCR_NORMALIZE_FAILED", want: true},
		{name: "image ocr recognize", toolID: "tool.image.ocr", detailCode: "IMAGE_OCR_RECOGNIZE_FAILED", want: true},
		{name: "image ocr publish", toolID: "tool.image.ocr", detailCode: "IMAGE_OCR_PUBLISH_FAILED", want: true},

		// image.background: Python bridge process and output I/O.
		{name: "image background single execution", toolID: "tool.image.background", detailCode: "IMAGE_BACKGROUND_EXECUTION", want: true},
		{name: "image background batch item", toolID: "tool.image.background", detailCode: "IMAGE_BACKGROUND_BATCH_ITEM", want: true},
		{name: "image background process", toolID: "tool.image.background", detailCode: "IMAGE_BACKGROUND_PROCESS_FAILED", want: true},
		{name: "image background publish", toolID: "tool.image.background", detailCode: "IMAGE_BACKGROUND_PUBLISH_FAILED", want: true},

		// image.watermark: in-process render/write.
		{name: "image watermark single execution", toolID: "tool.image.watermark", detailCode: "IMAGE_WATERMARK_EXECUTION", want: true},
		{name: "image watermark batch item", toolID: "tool.image.watermark", detailCode: "IMAGE_WATERMARK_BATCH_ITEM", want: true},

		// pdf.optimize: pdfcpu/Ghostscript execution and output write.
		{name: "pdf optimize", toolID: "tool.pdf.optimize", detailCode: "PDF_OPTIMIZE_FAILED", want: true},

		// pdf.ocr: OCR process failures and output I/O.
		{name: "pdf ocr rasterize", toolID: "tool.pdf.ocr", detailCode: "OCR_RASTERIZE_FAILED", want: true},
		{name: "pdf ocr recognize", toolID: "tool.pdf.ocr", detailCode: "OCR_RECOGNIZE_FAILED", want: true},
		{name: "pdf ocr merge", toolID: "tool.pdf.ocr", detailCode: "OCR_MERGE_FAILED", want: true},
		{name: "pdf ocr publish", toolID: "tool.pdf.ocr", detailCode: "OCR_PUBLISH_FAILED", want: true},

		// pdf.images: image decode and generated PDF write.
		{name: "pdf images decode", toolID: "tool.pdf.images", detailCode: "PDF_IMAGES_DECODE_FAILED", want: true},
		{name: "pdf images write", toolID: "tool.pdf.images", detailCode: "PDF_IMAGES_WRITE_FAILED", want: true},

		// pdf.rotate/remove_pages/reorder/extract_images: pdfcpu execution.
		{name: "pdf rotate", toolID: "tool.pdf.rotate", detailCode: "PDF_ROTATE_FAILED", want: true},
		{name: "pdf remove pages", toolID: "tool.pdf.remove_pages", detailCode: "PDF_REMOVE_PAGES_FAILED", want: true},
		{name: "pdf reorder", toolID: "tool.pdf.reorder", detailCode: "PDF_REORDER_FAILED", want: true},
		{name: "pdf extract images", toolID: "tool.pdf.extract_images", detailCode: "PDF_EXTRACT_IMAGES_FAILED", want: true},

		// pdf.to_docx: converter process failure.
		{name: "pdf to docx conversion", toolID: "tool.pdf.to_docx", detailCode: "PDF_DOCX_CONVERSION_FAILED", want: true},

		// video.transcribe: audio extraction, Whisper and subtitle write.
		{name: "transcribe audio", toolID: "tool.video.transcribe", detailCode: "TRANSCRIBE_AUDIO_FAILED", want: true},
		{name: "transcribe whisper", toolID: "tool.video.transcribe", detailCode: "TRANSCRIBE_FAILED", want: true},
		{name: "transcribe output", toolID: "tool.video.transcribe", detailCode: "TRANSCRIBE_OUTPUT_FAILED", want: true},

		// audio: ffmpeg encode and loudness failures are transient.
		{name: "audio extract execution", toolID: "tool.audio.extract", detailCode: "AUDIO_EXECUTION_FAILED", want: true},
		{name: "audio convert execution", toolID: "tool.audio.convert", detailCode: "AUDIO_EXECUTION_FAILED", want: true},
		{name: "audio normalize execution", toolID: "tool.audio.normalize", detailCode: "AUDIO_EXECUTION_FAILED", want: true},
		{name: "audio normalize loudness", toolID: "tool.audio.normalize", detailCode: "AUDIO_LOUDNORM_FAILED", want: true},

		// Deterministic image codes never retry.
		{name: "image crop validation", toolID: "tool.image.crop", detailCode: "IMAGE_CROP_DIMENSIONS_INVALID", want: false},
		{name: "image compress invalid target", toolID: "tool.image.compress", detailCode: "IMAGE_COMPRESS_TARGET_INVALID", want: false},
		{name: "image compress unsupported input", toolID: "tool.image.compress", detailCode: "IMAGE_COMPRESS_INPUT_UNSUPPORTED", want: false},
		{name: "image ocr runtime", toolID: "tool.image.ocr", detailCode: "IMAGE_OCR_RUNTIME_UNAVAILABLE", want: false},
		{name: "image ocr verify", toolID: "tool.image.ocr", detailCode: "IMAGE_OCR_VERIFY_FAILED", want: false},
		{name: "image background model", toolID: "tool.image.background", detailCode: "IMAGE_BACKGROUND_MODEL_UNAVAILABLE", want: false},
		{name: "image background verify", toolID: "tool.image.background", detailCode: "IMAGE_BACKGROUND_VERIFY_FAILED", want: false},
		{name: "image watermark validation", toolID: "tool.image.watermark", detailCode: "IMAGE_WATERMARK_TEXT_REQUIRED", want: false},
		{name: "image annotate unsupported operation", toolID: "tool.image.annotate", detailCode: "IMAGE_ANNOTATE_OPERATION_UNSUPPORTED", want: false},

		// Deterministic PDF codes never retry.
		{name: "pdf split validation", toolID: "tool.pdf.split", detailCode: "PDF_SPLIT_RANGES_INVALID", want: false},
		{name: "pdf merge protected", toolID: "tool.pdf.merge", detailCode: "PDF_PROTECTED_INPUT", want: false},
		{name: "pdf optimize target", toolID: "tool.pdf.optimize", detailCode: "PDF_OPTIMIZE_TARGET_INVALID", want: false},
		{name: "pdf ocr runtime", toolID: "tool.pdf.ocr", detailCode: "OCR_RUNTIME_UNAVAILABLE", want: false},
		{name: "pdf ocr verify", toolID: "tool.pdf.ocr", detailCode: "OCR_VERIFY_FAILED", want: false},
		{name: "pdf images unsupported", toolID: "tool.pdf.images", detailCode: "PDF_IMAGES_UNSUPPORTED_FORMAT", want: false},
		{name: "pdf images verify", toolID: "tool.pdf.images", detailCode: "PDF_IMAGES_VERIFY_FAILED", want: false},
		{name: "pdf to docx runtime", toolID: "tool.pdf.to_docx", detailCode: "PDF_DOCX_RUNTIME_UNAVAILABLE", want: false},
		{name: "pdf rotate angle", toolID: "tool.pdf.rotate", detailCode: "PDF_ROTATE_ANGLE_INVALID", want: false},
		{name: "pdf remove all pages", toolID: "tool.pdf.remove_pages", detailCode: "PDF_REMOVE_PAGES_ALL_PAGES", want: false},
		{name: "pdf reorder missing", toolID: "tool.pdf.reorder", detailCode: "PDF_REORDER_PAGE_MISSING", want: false},
		{name: "pdf extract none", toolID: "tool.pdf.extract_images", detailCode: "PDF_EXTRACT_IMAGES_NONE", want: false},
		{name: "pdf extract count mismatch", toolID: "tool.pdf.extract_images", detailCode: "PDF_EXTRACT_IMAGES_COUNT_MISMATCH", want: false},

		// Deterministic DOC codes never retry.
		{name: "docx runtime missing", toolID: "tool.doc.docx_to_pdf", detailCode: "DOC_DOCX_TO_PDF_RUNTIME_GHOSTSCRIPT_MISSING", want: false},
		{name: "docx protected", toolID: "tool.doc.docx_to_pdf", detailCode: "DOC_DOCX_TO_PDF_INPUT_PROTECTED", want: false},
		{name: "docx pdfa unsupported", toolID: "tool.doc.docx_to_pdf", detailCode: "DOC_DOCX_TO_PDF_PDFA_UNSUPPORTED", want: false},
		{name: "markdown validation", toolID: "tool.doc.md_to_pdf", detailCode: "DOC_MD_TO_PDF_CONFIG_INVALID", want: false},

		// Deterministic video codes never retry.
		{name: "video runtime missing", toolID: "tool.video.convert", detailCode: "VIDEO_RUNTIME_UNAVAILABLE", want: false},
		{name: "video format mismatch", toolID: "tool.video.convert", detailCode: "VIDEO_CONVERT_FORMAT_MISMATCH", want: false},
		{name: "video trim validation", toolID: "tool.video.trim", detailCode: "VIDEO_TRIM_INVALID_TIME_RANGE", want: false},
		{name: "video merge validation", toolID: "tool.video.merge", detailCode: "VIDEO_MERGE_INSUFFICIENT_INPUTS", want: false},
		{name: "transcribe runtime missing", toolID: "tool.video.transcribe", detailCode: "TRANSCRIBE_RUNTIME_UNAVAILABLE", want: false},
		{name: "transcribe model missing", toolID: "tool.video.transcribe", detailCode: "TRANSCRIBE_MODEL_MISSING", want: false},

		// Deterministic audio codes never retry.
		{name: "audio runtime missing", toolID: "tool.audio.convert", detailCode: "AUDIO_RUNTIME_UNAVAILABLE", want: false},
		{name: "audio encoder missing", toolID: "tool.audio.extract", detailCode: "AUDIO_ENCODER_UNAVAILABLE", want: false},
		{name: "audio unsupported input", toolID: "tool.audio.convert", detailCode: "AUDIO_INPUT_UNSUPPORTED", want: false},
		{name: "audio unsupported format", toolID: "tool.audio.convert", detailCode: "AUDIO_FORMAT_UNSUPPORTED", want: false},
		{name: "audio validation", toolID: "tool.audio.normalize", detailCode: "AUDIO_TARGET_LUFS_INVALID", want: false},

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

func TestIsRetryableErrorNeverRetriesCancellation(t *testing.T) {
	tests := []struct {
		name    string
		jobErr  *models.JobError
		comment string
	}{
		{name: "canonical cancelled by user", jobErr: &models.JobError{Code: models.ErrorCodeCancelledByUser}},
		{name: "canceled spelling", jobErr: &models.JobError{Code: "CANCELED"}},
		{name: "job cancelled lifecycle", jobErr: &models.JobError{Code: "JOB_CANCELLED"}},
		{name: "image detail cancelled", jobErr: &models.JobError{Code: "IMAGE_COMPRESS_EXECUTION", DetailCode: "IMAGE_COMPRESS_CANCELLED"}},
		{name: "doc detail cancelled", jobErr: &models.JobError{Code: "DOC_DOCX_TO_PDF_EXECUTION_FAILED", DetailCode: "DOC_DOCX_TO_PDF_CANCELLED"}},
		{name: "canceled code wins over retryable detail", jobErr: &models.JobError{Code: "CANCELED", DetailCode: "IMAGE_COMPRESS_EXECUTION"}},
		{name: "cancelled code wins over retryable detail", jobErr: &models.JobError{Code: "CANCELLED", DetailCode: "VIDEO_CONVERT_EXECUTION_FAILED"}},
	}

	for _, toolID := range registeredToolIDs {
		for _, tt := range tests {
			t.Run(toolID+"/"+tt.name, func(t *testing.T) {
				if isRetryableError(toolID, tt.jobErr) {
					t.Fatalf("isRetryableError(%q, %+v) retried a cancellation", toolID, tt.jobErr)
				}
			})
		}
	}
}

func TestIsRetryableErrorUnknownCodeIsNotRetryable(t *testing.T) {
	// Regression guard: an unclassified detail code must never be treated as a
	// transient failure.
	for _, toolID := range registeredToolIDs {
		t.Run(toolID, func(t *testing.T) {
			jobErr := &models.JobError{Code: "EXEC_IO_TRANSIENT", DetailCode: "FUTURE_UNKNOWN_FAILURE"}
			if isRetryableError(toolID, jobErr) {
				t.Fatalf("unknown code retried for %s", toolID)
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
