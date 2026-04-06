package jobs

import (
	"context"
	"strings"
	"time"

	"fileforge-desktop/internal/models"
)

const maxRetryAttempts = 3

var retryBackoffSchedule = []time.Duration{
	250 * time.Millisecond,
	500 * time.Millisecond,
	1000 * time.Millisecond,
}

func isRetryableError(toolID string, jobErr *models.JobErrorV1) bool {
	if jobErr == nil {
		return false
	}

	detailCode := strings.ToUpper(strings.TrimSpace(jobErr.DetailCode))
	if detailCode == "" {
		detailCode = strings.ToUpper(strings.TrimSpace(jobErr.Code))
	}

	transientByTool := map[string]map[string]struct{}{
		"tool.doc.docx_to_pdf": {
			"DOC_DOCX_TO_PDF_EXECUTION_FAILED":          {},
			"DOC_DOCX_TO_PDF_PRIMARY_EXECUTION_FAILED":  {},
			"DOC_DOCX_TO_PDF_FALLBACK_EXECUTION_FAILED": {},
		},
		"tool.doc.md_to_pdf": {
			"DOC_MD_TO_PDF_RENDER_FAILED": {},
		},
		"tool.pdf.crop": {
			"PDF_CROP_EXECUTION": {},
			"PDF_CROP_FAILED":    {},
		},
		"tool.pdf.merge": {
			"PDF_MERGE_EXECUTION": {},
			"PDF_MERGE_FAILED":    {},
		},
		"tool.pdf.split": {
			"PDF_SPLIT_EXECUTION": {},
			"PDF_SPLIT_FAILED":    {},
		},
		"tool.image.crop": {
			"IMAGE_CROP_EXECUTION": {},
		},
		"tool.image.annotate": {
			"IMAGE_ANNOTATE_EXECUTION":  {},
			"IMAGE_ANNOTATE_BATCH_ITEM": {},
		},
		"tool.video.convert": {
			"VIDEO_CONVERT_EXECUTION": {},
			"VIDEO_CONVERT_FAILED":    {},
		},
		"tool.video.trim": {
			"VIDEO_TRIM_EXECUTION": {},
			"VIDEO_TRIM_FAILED":    {},
		},
		"tool.video.merge": {
			"VIDEO_MERGE_EXECUTION": {},
			"VIDEO_MERGE_FAILED":    {},
		},
	}

	if byDetail, ok := transientByTool[toolID]; ok {
		_, matched := byDetail[detailCode]
		return matched
	}

	return false
}

func retryMetadata(attempts int, jobErr *models.JobErrorV1) map[string]any {
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
