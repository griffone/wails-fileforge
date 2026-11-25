package jobs

import (
	"context"
	"strings"
	"time"

	"fileforge-desktop/internal/models"
)

var retryBackoffSchedule = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	1000 * time.Millisecond,
}

// transientDetailCodesByTool is the explicit retry allowlist. A tool absent
// from the map never retries: only a detail code that represents a transient
// execution failure (I/O, timeout or an interrupted process) belongs here.
// Validation, unsupported-format, protected-input, missing-dependency and
// cancellation codes are deliberately excluded.
var transientDetailCodesByTool = map[string]map[string]struct{}{
	// Encode/read/write failures for the single and batch item paths.
	"tool.image.convert": {
		"IMAGE_BATCH_ITEM":       {}, // per-item encode/read/write failure
		"IMAGE_SINGLE_EXECUTION": {}, // single-image encode/read/write failure
	},
	// Decode/crop/write failures for the single and batch item paths.
	"tool.image.crop": {
		"IMAGE_CROP_BATCH_ITEM": {}, // per-item decode/crop/write failure
		"IMAGE_CROP_EXECUTION":  {}, // single-image decode/crop/write failure
	},
	// Render/write failures for the single and batch item paths.
	"tool.image.annotate": {
		"IMAGE_ANNOTATE_BATCH_ITEM": {}, // per-item render/write failure
		"IMAGE_ANNOTATE_EXECUTION":  {}, // single-image render/write failure
	},
	// IMAGE_COMPRESS_EXECUTION and IMAGE_COMPRESS_BATCH_ITEM cover read,
	// encode and write failures for single and batch compression.
	"tool.image.compress": {
		"IMAGE_COMPRESS_EXECUTION":  {},
		"IMAGE_COMPRESS_BATCH_ITEM": {},
	},
	// IMAGE_OCR_* execution codes cover Tesseract process failures and output
	// I/O. Verification failures stay deterministic and are not listed.
	"tool.image.ocr": {
		"IMAGE_OCR_EXECUTION":        {},
		"IMAGE_OCR_BATCH_ITEM":       {},
		"IMAGE_OCR_NORMALIZE_FAILED": {},
		"IMAGE_OCR_RECOGNIZE_FAILED": {},
		"IMAGE_OCR_PUBLISH_FAILED":   {},
	},
	// Background removal failures come from the Python bridge process and from
	// publishing its output. Runtime/model/verification failures are excluded.
	"tool.image.background": {
		"IMAGE_BACKGROUND_EXECUTION":      {},
		"IMAGE_BACKGROUND_BATCH_ITEM":     {},
		"IMAGE_BACKGROUND_PROCESS_FAILED": {},
		"IMAGE_BACKGROUND_PUBLISH_FAILED": {},
	},
	// Watermarks render in-process; the only transient outcome is the generic
	// render/write failure for a single item or a batch item.
	"tool.image.watermark": {
		"IMAGE_WATERMARK_EXECUTION":  {},
		"IMAGE_WATERMARK_BATCH_ITEM": {},
	},
	// pdfcpu read/merge/write failures are transient; input validation and
	// protected-file codes are not.
	"tool.pdf.merge": {
		"PDF_MERGE_FAILED": {}, // pdfcpu read/merge/write failure
	},
	// pdfcpu read/split/write failures; strategy and range validation are not.
	"tool.pdf.split": {
		"PDF_SPLIT_FAILED": {}, // pdfcpu read/split/write failure
	},
	// pdfcpu read/crop/write failures; margins and page selection are not.
	"tool.pdf.crop": {
		"PDF_CROP_FAILED": {}, // pdfcpu read/crop/write failure
	},
	// PDF_OPTIMIZE_FAILED covers the pdfcpu/Ghostscript execution and output
	// write. Target, metadata and output-exists codes are deterministic.
	"tool.pdf.optimize": {
		"PDF_OPTIMIZE_FAILED": {},
	},
	// OCR process failures and output I/O. Runtime, language, verification and
	// output-path codes are excluded.
	"tool.pdf.ocr": {
		"OCR_RASTERIZE_FAILED": {},
		"OCR_RECOGNIZE_FAILED": {},
		"OCR_MERGE_FAILED":     {},
		"OCR_PUBLISH_FAILED":   {},
	},
	"tool.pdf.images": {
		"PDF_IMAGES_DECODE_FAILED": {}, // image decode/read failure
		"PDF_IMAGES_WRITE_FAILED":  {}, // generated PDF write failure
	},
	// PDF_DOCX_CONVERSION_FAILED covers the converter process and the generated
	// file. Invalid-input and output-path failures are deterministic.
	"tool.pdf.to_docx": {
		"PDF_DOCX_CONVERSION_FAILED": {},
	},
	// Fonts substitution is a warning code; the transient entries are the
	// renderer's process and output write failures.
	"tool.doc.docx_to_pdf": {
		"DOC_DOCX_TO_PDF_PRIMARY_EXECUTION_FAILED": {}, // renderer process/write failure
		"DOC_DOCX_TO_PDF_EXECUTION_FAILED":         {}, // generic renderer failure
	},
	// Markdown rendering can fail transiently while writing the PDF.
	"tool.doc.md_to_pdf": {
		"DOC_MD_TO_PDF_RENDER_FAILED": {}, // renderer process/write failure
	},
	// FFmpeg process failures are transient; format, path and validation codes
	// are not.
	"tool.video.convert": {
		"VIDEO_CONVERT_EXECUTION_FAILED": {}, // ffmpeg process failure
	},
	"tool.video.trim": {
		"VIDEO_TRIM_EXECUTION_FAILED": {}, // ffmpeg process failure
	},
	"tool.video.merge": {
		"VIDEO_MERGE_EXECUTION_FAILED": {}, // ffmpeg process failure
	},
	// Audio extraction, Whisper execution and subtitle writing are the
	// transient stages. Runtime, model and validation codes are excluded.
	"tool.video.transcribe": {
		"TRANSCRIBE_AUDIO_FAILED":  {},
		"TRANSCRIBE_FAILED":        {},
		"TRANSCRIBE_OUTPUT_FAILED": {},
	},
}

func isRetryableError(toolID string, jobErr *models.JobError) bool {
	if jobErr == nil {
		return false
	}

	// Cancellation never retries, whatever code shape carries it.
	if isCancellationJobError(jobErr) {
		return false
	}

	detailCode := strings.ToUpper(strings.TrimSpace(jobErr.DetailCode))
	if detailCode == "" {
		detailCode = strings.ToUpper(strings.TrimSpace(jobErr.Code))
	}

	byDetail, ok := transientDetailCodesByTool[toolID]
	if !ok {
		return false
	}

	_, matched := byDetail[detailCode]
	return matched
}

// isCancellationJobError reports whether any code field on the error is a
// cancellation, covering the CANCELLED and CANCELED spellings plus the engine's
// JOB_CANCELLED and CANCELLED_BY_USER lifecycle codes.
func isCancellationJobError(jobErr *models.JobError) bool {
	for _, candidate := range []string{jobErr.Code, jobErr.DetailCode} {
		normalized := strings.ToUpper(strings.TrimSpace(candidate))
		if normalized == "" {
			continue
		}
		if normalized == models.ErrorCodeCancelledByUser || normalized == "JOB_CANCELLED" {
			return true
		}
		if strings.Contains(normalized, "CANCELLED") || strings.Contains(normalized, "CANCELED") {
			return true
		}
	}

	return false
}

func retryMetadata(attempts int, jobErr *models.JobError) map[string]any {
	if attempts <= 1 {
		return nil
	}

	metadata := map[string]any{
		"attempts":   attempts,
		"retryCount": attempts - 1,
	}

	if jobErr != nil {
		metadata["lastErrorCode"] = jobErr.Code
		if jobErr.DetailCode != "" {
			metadata["lastDetailCode"] = jobErr.DetailCode
		}
	}

	return metadata
}

func waitRetryBackoff(ctx context.Context, attempt int) bool {
	if attempt <= 0 || attempt > len(retryBackoffSchedule) {
		return true
	}

	timer := time.NewTimer(retryBackoffSchedule[attempt-1])
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
