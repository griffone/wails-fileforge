package models

import "strings"

const (
	JobStatusQueued         = "queued"
	JobStatusRunning        = "running"
	JobStatusSuccess        = "success"
	JobStatusFailed         = "failed"
	JobStatusPartialSuccess = "partial_success"
	JobStatusCancelled      = "cancelled"
	JobStatusInterrupted    = "interrupted"
)

const (
	ErrorCodeValidationInvalidInput = "VALIDATION_INVALID_INPUT"
	ErrorCodeRuntimeDepMissing      = "RUNTIME_DEP_MISSING"
	ErrorCodeExecIOTransient        = "EXEC_IO_TRANSIENT"
	ErrorCodeExecTimeoutTransient   = "EXEC_TIMEOUT_TRANSIENT"
	ErrorCodeUnsupportedFormat      = "UNSUPPORTED_FORMAT"
	ErrorCodeCancelledByUser        = "CANCELLED_BY_USER"

	// ErrorCodeUnclassified is the conservative fallback for a detail code that
	// has no canonical family. It exists so an unknown code is never reported as
	// a transient failure: consumers must treat it as a deterministic failure
	// (never retry it automatically) until the code is classified explicitly.
	ErrorCodeUnclassified = "ERROR_UNCLASSIFIED"
)

func NewJobError(code, detailCode, message string, details map[string]any) *JobError {
	err := &JobError{
		Code:       code,
		DetailCode: detailCode,
		Message:    message,
		Details:    details,
	}

	if len(err.Details) == 0 {
		err.Details = nil
	}

	return err
}

func NewCanonicalJobError(detailCode, message string, details map[string]any) *JobError {
	return NewJobError(CanonicalErrorCode(detailCode), detailCode, message, details)
}

// canonicalCodeFamilies lists the stable detail codes a domain can emit, split
// by the canonical family they belong to. Codes are enumerated instead of
// inferred from suffixes because a suffix alone cannot tell a deterministic
// failure (IMAGE_CROP_COORDINATES_INVALID) from a transient one
// (IMAGE_CROP_BATCH_ITEM). Every code listed here is reachable from a job error
// constructor in that domain.
type canonicalCodeFamilies struct {
	validation  []string
	runtime     []string
	unsupported []string
	cancelled   []string
	execution   []string
	// unclassified lists codes that have no canonical family yet. They map to
	// ErrorCodeUnclassified on purpose; document why before moving one.
	unclassified []string
}

var canonicalImageCodes = canonicalCodeFamilies{
	validation: []string{
		"IMAGE_ANNOTATE_ARROW_INVALID",
		"IMAGE_ANNOTATE_BATCH_INPUT_REQUIRED",
		"IMAGE_ANNOTATE_BLUR_INTENSITY_INVALID",
		"IMAGE_ANNOTATE_COORDINATES_INVALID",
		"IMAGE_ANNOTATE_DIMENSIONS_INVALID",
		"IMAGE_ANNOTATE_FONT_SIZE_INVALID",
		"IMAGE_ANNOTATE_INPUT_REQUIRED",
		"IMAGE_ANNOTATE_MODE_INVALID",
		"IMAGE_ANNOTATE_OPACITY_INVALID",
		"IMAGE_ANNOTATE_OPERATION_INVALID",
		"IMAGE_ANNOTATE_OPERATIONS_EMPTY",
		"IMAGE_ANNOTATE_OPERATIONS_INVALID",
		"IMAGE_ANNOTATE_OPERATION_TYPE_REQUIRED",
		"IMAGE_ANNOTATE_OPTION_REQUIRED",
		"IMAGE_ANNOTATE_OUTPUT_COLLIDES_INPUT",
		"IMAGE_ANNOTATE_OUTPUT_DIR_INVALID",
		"IMAGE_ANNOTATE_OUTPUT_EXISTS",
		"IMAGE_ANNOTATE_OUT_OF_BOUNDS",
		"IMAGE_ANNOTATE_PREVIEW_INVALID_PATH",
		"IMAGE_ANNOTATE_REDACT_COLOR_REQUIRED",
		"IMAGE_ANNOTATE_SINGLE_INPUT_COUNT",
		"IMAGE_ANNOTATE_STROKE_INVALID",
		"IMAGE_ANNOTATE_TEXT_REQUIRED",
		"IMAGE_BACKGROUND_INPUT_INVALID",
		"IMAGE_BACKGROUND_INPUT_NOT_FOUND",
		"IMAGE_BACKGROUND_INVALID",
		"IMAGE_BACKGROUND_MODE_INVALID",
		"IMAGE_BACKGROUND_OPTION_INVALID",
		"IMAGE_BACKGROUND_OUTPUT_ALREADY_EXISTS",
		"IMAGE_BACKGROUND_OUTPUT_INVALID",
		"IMAGE_BACKGROUND_SINGLE_INPUT_INVALID",
		"IMAGE_UPSCALE_FORMAT_INVALID",
		"IMAGE_UPSCALE_INPUT_INVALID",
		"IMAGE_UPSCALE_INPUT_NOT_FOUND",
		"IMAGE_UPSCALE_MODE_INVALID",
		"IMAGE_UPSCALE_MODEL_INVALID",
		"IMAGE_UPSCALE_OPTION_INVALID",
		"IMAGE_UPSCALE_OUTPUT_ALREADY_EXISTS",
		"IMAGE_UPSCALE_OUTPUT_INVALID",
		"IMAGE_UPSCALE_OUTPUT_TOO_LARGE",
		"IMAGE_UPSCALE_SINGLE_INPUT_INVALID",
		"IMAGE_COMPRESS_BACKGROUND_INVALID",
		"IMAGE_COMPRESS_FORMAT_INVALID",
		"IMAGE_COMPRESS_INPUT_NOT_FOUND",
		"IMAGE_COMPRESS_INPUT_REQUIRED",
		"IMAGE_COMPRESS_MODE_INVALID",
		"IMAGE_COMPRESS_OPTION_INVALID",
		"IMAGE_COMPRESS_OUTPUT_COLLIDES_INPUT",
		"IMAGE_COMPRESS_OUTPUT_DIR_INVALID",
		"IMAGE_COMPRESS_OUTPUT_EXISTS",
		"IMAGE_COMPRESS_OUTPUT_EXTENSION_MISMATCH",
		"IMAGE_COMPRESS_SINGLE_INPUT_COUNT",
		"IMAGE_COMPRESS_TARGET_INVALID",
		"IMAGE_COMPRESS_TARGET_REQUIRED",
		"IMAGE_CROP_BATCH_INPUT_REQUIRED",
		"IMAGE_CROP_COORDINATES_INVALID",
		"IMAGE_CROP_DIMENSIONS_INVALID",
		"IMAGE_CROP_INPUT_NOT_FOUND",
		"IMAGE_CROP_INPUT_REQUIRED",
		"IMAGE_CROP_MODE_INVALID",
		"IMAGE_CROP_OPTION_INVALID",
		"IMAGE_CROP_OPTION_REQUIRED",
		"IMAGE_CROP_OUTPUT_COLLIDES_INPUT",
		"IMAGE_CROP_OUTPUT_DIR_INVALID",
		"IMAGE_CROP_OUTPUT_EXISTS",
		"IMAGE_CROP_OUTPUT_SIZE_INVALID",
		"IMAGE_CROP_OUT_OF_BOUNDS",
		"IMAGE_CROP_PREVIEW_INVALID_PATH",
		"IMAGE_CROP_RATIO_INVALID",
		"IMAGE_CROP_ROTATE_INVALID",
		"IMAGE_CROP_SINGLE_INPUT_COUNT",
		"IMAGE_CROP_SUGGESTION_INVALID_PATH",
		"IMAGE_FORMAT_INVALID",
		"IMAGE_INPUT_NOT_FOUND",
		"IMAGE_INPUT_REQUIRED",
		"IMAGE_MODE_INVALID",
		"IMAGE_OCR_DPI_INVALID",
		"IMAGE_OCR_INPUT_NOT_FOUND",
		"IMAGE_OCR_INPUT_REQUIRED",
		"IMAGE_OCR_LANGUAGE_INVALID",
		"IMAGE_OCR_MODE_INVALID",
		"IMAGE_OCR_OPTION_INVALID",
		"IMAGE_OCR_OUTPUT_ALREADY_EXISTS",
		"IMAGE_OCR_OUTPUT_DIR_INVALID",
		"IMAGE_OCR_OUTPUT_INVALID",
		"IMAGE_OCR_SINGLE_INPUT_COUNT",
		"IMAGE_OUTPUT_DIR_INVALID",
		"IMAGE_OUTPUT_EXISTS",
		"IMAGE_OUTPUT_SAME_AS_INPUT",
		"IMAGE_PREVIEW_INVALID_PATH",
		"IMAGE_SINGLE_INPUT_COUNT",
		"IMAGE_WATERMARK_ANCHOR_INVALID",
		"IMAGE_WATERMARK_COLOR_INVALID",
		"IMAGE_WATERMARK_FORMAT_INVALID",
		"IMAGE_WATERMARK_INPUT_NOT_FOUND",
		"IMAGE_WATERMARK_INPUT_REQUIRED",
		"IMAGE_WATERMARK_KIND_INVALID",
		"IMAGE_WATERMARK_LOGO_INVALID",
		"IMAGE_WATERMARK_LOGO_NOT_FOUND",
		"IMAGE_WATERMARK_LOGO_REQUIRED",
		"IMAGE_WATERMARK_MARGIN_INVALID",
		"IMAGE_WATERMARK_MODE_INVALID",
		"IMAGE_WATERMARK_OPACITY_INVALID",
		"IMAGE_WATERMARK_OPTION_INVALID",
		"IMAGE_WATERMARK_OUTPUT_COLLIDES_INPUT",
		"IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
		"IMAGE_WATERMARK_OUTPUT_EXTENSION_MISMATCH",
		"IMAGE_WATERMARK_OUTPUT_EXISTS",
		"IMAGE_WATERMARK_QUALITY_INVALID",
		"IMAGE_WATERMARK_ROTATION_INVALID",
		"IMAGE_WATERMARK_SCALE_INVALID",
		"IMAGE_WATERMARK_SINGLE_INPUT_COUNT",
		"IMAGE_WATERMARK_TEXT_CONTROL_CHARS",
		"IMAGE_WATERMARK_TEXT_DOES_NOT_FIT",
		"IMAGE_WATERMARK_TEXT_REQUIRED",
		"IMAGE_WATERMARK_TEXT_TOO_LONG",
		"IMAGE_WATERMARK_TEXT_TOO_MANY_LINES",
	},
	runtime: []string{
		"IMAGE_BACKGROUND_MODEL_UNAVAILABLE",
		"IMAGE_BACKGROUND_RUNTIME_UNAVAILABLE",
		"IMAGE_OCR_LANGUAGE_UNAVAILABLE",
		"IMAGE_OCR_RUNTIME_UNAVAILABLE",
		"IMAGE_UPSCALE_MODEL_UNAVAILABLE",
		"IMAGE_UPSCALE_RUNTIME_UNAVAILABLE",
	},
	unsupported: []string{
		"IMAGE_ANNOTATE_OPERATION_UNSUPPORTED",
		"IMAGE_BACKGROUND_INPUT_UNSUPPORTED",
		"IMAGE_COMPRESS_INPUT_UNSUPPORTED",
		"IMAGE_CROP_FORMAT_UNSUPPORTED",
		"IMAGE_CROP_INPUT_UNSUPPORTED",
		"IMAGE_INPUT_UNSUPPORTED",
		"IMAGE_OCR_INPUT_UNSUPPORTED",
		"IMAGE_UPSCALE_INPUT_UNSUPPORTED",
		"IMAGE_WATERMARK_INPUT_UNSUPPORTED",
		"IMAGE_WATERMARK_LOGO_UNSUPPORTED",
		"IMAGE_WATERMARK_TEXT_UNSUPPORTED",
		"IMAGE_WATERMARK_TILE_UNSUPPORTED",
	},
	cancelled: []string{
		"IMAGE_ANNOTATE_CANCELLED",
		"IMAGE_BACKGROUND_CANCELLED",
		"IMAGE_BATCH_CANCELLED",
		"IMAGE_COMPRESS_CANCELLED",
		"IMAGE_CROP_CANCELLED",
		"IMAGE_OCR_CANCELLED",
		"IMAGE_UPSCALE_CANCELLED",
		"IMAGE_WATERMARK_CANCELLED",
	},
	execution: []string{
		"IMAGE_ANNOTATE_BATCH_ITEM",
		"IMAGE_ANNOTATE_EXECUTION",
		"IMAGE_ANNOTATE_PREVIEW_EXECUTION",
		"IMAGE_ANNOTATE_PREVIEW_READ_FAILED",
		"IMAGE_ANNOTATE_READ_FAILED",
		"IMAGE_BACKGROUND_BATCH_ITEM",
		"IMAGE_BACKGROUND_EXECUTION",
		"IMAGE_BACKGROUND_PROCESS_FAILED",
		"IMAGE_BACKGROUND_PUBLISH_FAILED",
		"IMAGE_BATCH_ITEM",
		"IMAGE_COMPRESS_BATCH_ITEM",
		"IMAGE_COMPRESS_EXECUTION",
		"IMAGE_CROP_BATCH_ITEM",
		"IMAGE_CROP_DIMENSIONS_READ_FAILED",
		"IMAGE_CROP_EXECUTION",
		"IMAGE_CROP_PREVIEW_EXECUTION",
		"IMAGE_CROP_PREVIEW_READ_FAILED",
		"IMAGE_CROP_READ_FAILED",
		"IMAGE_CROP_SUGGESTION_DECODE_FAILED",
		"IMAGE_CROP_SUGGESTION_READ_FAILED",
		"IMAGE_OCR_BATCH_ITEM",
		"IMAGE_OCR_EXECUTION",
		"IMAGE_OCR_NORMALIZE_FAILED",
		"IMAGE_OCR_PUBLISH_FAILED",
		"IMAGE_OCR_RECOGNIZE_FAILED",
		"IMAGE_PREVIEW_READ_FAILED",
		"IMAGE_SINGLE_EXECUTION",
		"IMAGE_UPSCALE_BATCH_ITEM",
		"IMAGE_UPSCALE_EXECUTION",
		"IMAGE_UPSCALE_PROCESS_FAILED",
		"IMAGE_UPSCALE_PUBLISH_FAILED",
		"IMAGE_WATERMARK_BATCH_ITEM",
		"IMAGE_WATERMARK_EXECUTION",
	},
	unclassified: []string{
		// A missed output is deterministic: the encoder or its runtime produced
		// nothing, so retrying the same input cannot help.
		"IMAGE_BACKGROUND_VERIFY_FAILED",
		"IMAGE_OCR_VERIFY_FAILED",
		// Rollback bookkeeping, not an execution failure of this item.
		"IMAGE_BACKGROUND_BATCH_FAILED",
	},
}

var canonicalPDFCodes = canonicalCodeFamilies{
	validation: []string{
		"OCR_NO_TEXT",
		"OCR_OUTPUT_ALREADY_EXISTS",
		"OCR_OUTPUT_DIR_INVALID",
		"PDF_CROP_MARGINS_INVALID",
		"PDF_CROP_MARGINS_REQUIRED",
		"PDF_CROP_OUTPUT_ALREADY_EXISTS",
		"PDF_CROP_PAGE_SELECTION_INVALID",
		"PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS",
		"PDF_CROP_PRESET_INVALID",
		"PDF_DOCX_INVALID_INPUT",
		"PDF_DOCX_OUTPUT_FAILED",
		"PDF_DUPLICATE_INPUT",
		"PDF_EXTRACT_IMAGES_NONE",
		"PDF_EXTRACT_IMAGES_PAGE_SELECTION_INVALID",
		"PDF_EXTRACT_IMAGES_PAGE_SELECTION_OUT_OF_BOUNDS",
		"PDF_IMAGES_DUPLICATE_INPUT",
		"PDF_IMAGES_INVALID_INPUT",
		"PDF_IMAGES_INVALID_LAYOUT",
		"PDF_IMAGES_OUTPUT_DIR_INVALID",
		"PDF_IMAGES_OUTPUT_EXISTS",
		"PDF_IMAGES_UNPLACED_IMAGES",
		"PDF_INVALID_INPUT",
		"PDF_INVALID_INPUTS",
		"PDF_MERGE_OUTPUT_ALREADY_EXISTS",
		"PDF_MERGE_PAGE_RANGE_INVALID",
		"PDF_OPTIMIZE_METADATA_INVALID",
		"PDF_OPTIMIZE_OUTPUT_ALREADY_EXISTS",
		"PDF_OPTIMIZE_TARGET_INVALID",
		"PDF_OUTPUT_COLLIDES_INPUT",
		"PDF_OUTPUT_ALREADY_EXISTS",
		"PDF_OUTPUT_DIR_NOT_DIRECTORY",
		"PDF_OUTPUT_DIR_NOT_FOUND",
		"PDF_OUTPUT_DIR_NOT_WRITABLE",
		"PDF_PREVIEW",
		"PDF_PREVIEW_INVALID_PATH",
		"PDF_PREVIEW_NOT_PDF",
		"PDF_PREVIEW_TOO_LARGE",
		"PDF_PROTECTED_INPUT",
		"PDF_REMOVE_PAGES_ALL_PAGES",
		"PDF_REMOVE_PAGES_SELECTION_INVALID",
		"PDF_REMOVE_PAGES_SELECTION_OUT_OF_BOUNDS",
		"PDF_REMOVE_PAGES_SELECTION_REQUIRED",
		"PDF_REORDER_ORDER_INVALID",
		"PDF_REORDER_ORDER_REQUIRED",
		"PDF_REORDER_PAGE_DUPLICATE",
		"PDF_REORDER_PAGE_MISSING",
		"PDF_REORDER_PAGE_OUT_OF_BOUNDS",
		"PDF_ROTATE_ANGLE_INVALID",
		"PDF_ROTATE_PAGE_SELECTION_INVALID",
		"PDF_ROTATE_PAGE_SELECTION_OUT_OF_BOUNDS",
		"PDF_SPLIT_BOOKMARKS_INVALID",
		"PDF_SPLIT_NO_BOOKMARKS",
		"PDF_SPLIT_OUTPUT_ALREADY_EXISTS",
		"PDF_SPLIT_OUTPUT_COLLISION",
		"PDF_SPLIT_PAGES_PER_FILE_INVALID",
		"PDF_SPLIT_RANGE_OUT_OF_BOUNDS",
		"PDF_SPLIT_RANGES_INVALID",
		"PDF_SPLIT_RANGES_REQUIRED",
	},
	runtime: []string{
		"OCR_LANGUAGE_UNAVAILABLE",
		"OCR_RUNTIME_UNAVAILABLE",
		"PDF_DOCX_RUNTIME_UNAVAILABLE",
	},
	unsupported: []string{
		"PDF_IMAGES_UNSUPPORTED_FORMAT",
		"PDF_SPLIT_STRATEGY_UNSUPPORTED",
	},
	cancelled: []string{
		"OCR_CANCELLED",
		"PDF_DOCX_CANCELLED",
		"PDF_IMAGES_CANCELLED",
	},
	execution: []string{
		"OCR_MERGE_FAILED",
		"OCR_PUBLISH_FAILED",
		"OCR_RASTERIZE_FAILED",
		"OCR_RECOGNIZE_FAILED",
		"PDF_CROP_EXECUTION_MISSING_RESULT",
		"PDF_CROP_FAILED",
		"PDF_DOCX_CONVERSION_FAILED",
		"PDF_EXTRACT_IMAGES_EXECUTION_MISSING_RESULT",
		"PDF_EXTRACT_IMAGES_FAILED",
		"PDF_IMAGES_DECODE_FAILED",
		"PDF_IMAGES_EXECUTION_MISSING_RESULT",
		"PDF_IMAGES_WRITE_FAILED",
		"PDF_MERGE_EXECUTION_MISSING_RESULT",
		"PDF_MERGE_FAILED",
		"PDF_OCR_EXECUTION_MISSING_RESULT",
		"PDF_OPTIMIZE_EXECUTION_MISSING_RESULT",
		"PDF_OPTIMIZE_FAILED",
		"PDF_PREVIEW_READ_FAILED",
		"PDF_REMOVE_PAGES_EXECUTION_MISSING_RESULT",
		"PDF_REMOVE_PAGES_FAILED",
		"PDF_REORDER_EXECUTION_MISSING_RESULT",
		"PDF_REORDER_FAILED",
		"PDF_ROTATE_EXECUTION_MISSING_RESULT",
		"PDF_ROTATE_FAILED",
		"PDF_SPLIT_EXECUTION_MISSING_RESULT",
		"PDF_SPLIT_FAILED",
		"PDF_TO_DOCX_EXECUTION_MISSING_RESULT",
	},
	unclassified: []string{
		// Verification failures mean the produced file does not match what was
		// written; they are deterministic, not transient.
		"OCR_VERIFY_FAILED",
		"PDF_EXTRACT_IMAGES_COUNT_MISMATCH",
		"PDF_IMAGES_VERIFY_FAILED",
		"PDF_REMOVE_PAGES_VERIFY_FAILED",
		"PDF_REORDER_VERIFY_FAILED",
		"PDF_ROTATE_VERIFY_FAILED",
	},
}

// canonicalOutputNameCodes classifies the failures of the output-name
// suggestion endpoint. An invalid path is user-fixable input; read and decode
// failures mean the file itself could not be inspected.
var canonicalOutputNameCodes = canonicalCodeFamilies{
	validation: []string{
		"OUTPUT_NAME_SUGGESTION_INVALID_PATH",
	},
	execution: []string{
		"OUTPUT_NAME_SUGGESTION_DECODE_FAILED",
		"OUTPUT_NAME_SUGGESTION_READ_FAILED",
	},
}

var canonicalVideoCodes = canonicalCodeFamilies{
	validation: []string{
		"VIDEO_AUDIO_SYNC_BITRATE_INVALID",
		"VIDEO_AUDIO_SYNC_DURATION_UNAVAILABLE",
		"VIDEO_AUDIO_SYNC_GAIN_INVALID",
		"VIDEO_AUDIO_SYNC_INPUT_NO_VIDEO",
		"VIDEO_AUDIO_SYNC_MODE_INVALID",
		"VIDEO_AUDIO_SYNC_OFFSET_INVALID",
		"VIDEO_AUDIO_SYNC_ORIGINAL_AUDIO_INVALID",
		"VIDEO_AUDIO_SYNC_ORIGINAL_AUDIO_MISSING",
		"VIDEO_AUDIO_SYNC_OUTPUT_COLLIDES_INPUT",
		"VIDEO_AUDIO_SYNC_OUTPUT_EXISTS",
		"VIDEO_AUDIO_SYNC_SEGMENTS_REQUIRED",
		"VIDEO_AUDIO_SYNC_SEGMENT_INVALID",
		"VIDEO_AUDIO_SYNC_SEGMENT_LIMIT",
		"VIDEO_AUDIO_SYNC_SEGMENT_OVERLAP",
		"VIDEO_AUDIO_SYNC_SOURCE_NO_AUDIO",
		"VIDEO_AUDIO_SYNC_SOURCE_NOT_FOUND",
		"VIDEO_AUDIO_SYNC_SOURCE_START_INVALID",
		"VIDEO_AUDIO_SYNC_VALIDATION_ERROR",
		"VIDEO_CONVERT_OUTPUT_COLLIDES_INPUT",
		"VIDEO_CONVERT_OUTPUT_DIR_NOT_DIRECTORY",
		"VIDEO_CONVERT_OUTPUT_DIR_NOT_FOUND",
		"VIDEO_CONVERT_OUTPUT_DIR_NOT_WRITABLE",
		"VIDEO_CONVERT_OUTPUT_EXISTS",
		"VIDEO_CONVERT_VALIDATION_ERROR",
		"VIDEO_MERGE_INSUFFICIENT_INPUTS",
		"VIDEO_MERGE_MODE_INVALID",
		"VIDEO_MERGE_OUTPUT_COLLIDES_INPUT",
		"VIDEO_MERGE_OUTPUT_DIR_NOT_DIRECTORY",
		"VIDEO_MERGE_OUTPUT_DIR_NOT_FOUND",
		"VIDEO_MERGE_OUTPUT_DIR_NOT_WRITABLE",
		"VIDEO_MERGE_OUTPUT_EXISTS",
		"VIDEO_MERGE_VALIDATION_ERROR",
		"VIDEO_TRIM_INVALID_TIME_RANGE",
		"VIDEO_TRIM_MODE_INVALID",
		"VIDEO_TRIM_OUTPUT_COLLIDES_INPUT",
		"VIDEO_TRIM_OUTPUT_DIR_NOT_DIRECTORY",
		"VIDEO_TRIM_OUTPUT_DIR_NOT_FOUND",
		"VIDEO_TRIM_OUTPUT_DIR_NOT_WRITABLE",
		"VIDEO_TRIM_OUTPUT_EXISTS",
		"VIDEO_TRIM_VALIDATION_ERROR",
	},
	runtime: []string{
		"BURN_RUNTIME_UNAVAILABLE",
		"TRANSCRIBE_MODEL_MISSING",
		"TRANSCRIBE_RUNTIME_UNAVAILABLE",
		"VIDEO_AUDIO_SYNC_CODEC_UNAVAILABLE",
		"VIDEO_CONVERT_CODEC_UNAVAILABLE",
		"VIDEO_MERGE_CODEC_UNAVAILABLE",
		"VIDEO_RUNTIME_FFMPEG_NOT_FOUND",
		"VIDEO_RUNTIME_FFPROBE_NOT_FOUND",
		"VIDEO_RUNTIME_UNAVAILABLE",
		"VIDEO_TRIM_CODEC_UNAVAILABLE",
	},
	unsupported: []string{
		"VIDEO_CONVERT_FORMAT_MISMATCH",
		"VIDEO_MERGE_FORMAT_MISMATCH",
		"VIDEO_TRIM_FORMAT_MISMATCH",
	},
	cancelled: []string{
		"BURN_CANCELLED",
		"TRANSCRIBE_CANCELLED",
		"VIDEO_AUDIO_SYNC_CANCELLED",
	},
	execution: []string{
		"BURN_FAILED",
		"BURN_OUTPUT_FAILED",
		"TRANSCRIBE_AUDIO_FAILED",
		"TRANSCRIBE_FAILED",
		"TRANSCRIBE_OUTPUT_FAILED",
		"VIDEO_AUDIO_SYNC_EXECUTION_FAILED",
		"VIDEO_AUDIO_SYNC_INPUT_OPEN_FAILED",
		"VIDEO_AUDIO_SYNC_LOUDNORM_FAILED",
		"VIDEO_AUDIO_SYNC_OUTPUT_WRITE_FAILED",
		"VIDEO_CONVERT_EXECUTION_FAILED",
		"VIDEO_CONVERT_INPUT_OPEN_FAILED",
		"VIDEO_CONVERT_OUTPUT_WRITE_FAILED",
		"VIDEO_MERGE_AUTO_FALLBACK_REENCODE_FAILED",
		"VIDEO_MERGE_COPY_FAILED",
		"VIDEO_MERGE_EXECUTION_FAILED",
		"VIDEO_MERGE_INPUT_OPEN_FAILED",
		"VIDEO_MERGE_OUTPUT_WRITE_FAILED",
		"VIDEO_TRIM_AUTO_FALLBACK_REENCODE_FAILED",
		"VIDEO_TRIM_COPY_FAILED",
		"VIDEO_TRIM_EXECUTION_FAILED",
		"VIDEO_TRIM_INPUT_OPEN_FAILED",
		"VIDEO_TRIM_OUTPUT_WRITE_FAILED",
	},
}

// canonicalAudioCodes classifies the audio engine's stable detail codes. Every
// code an audio tool can emit is enumerated so a deterministic failure never
// falls through to a transient or unknown family.
var canonicalAudioCodes = canonicalCodeFamilies{
	validation: []string{
		"AUDIO_BITRATE_INVALID",
		"AUDIO_INPUT_NOT_FOUND",
		"AUDIO_INPUT_REQUIRED",
		"AUDIO_OUTPUT_COLLIDES_INPUT",
		"AUDIO_OUTPUT_DIR_NOT_DIRECTORY",
		"AUDIO_OUTPUT_DIR_NOT_FOUND",
		"AUDIO_OUTPUT_DIR_NOT_WRITABLE",
		"AUDIO_OUTPUT_EXISTS",
		"AUDIO_TARGET_LUFS_INVALID",
		"AUDIO_TRUE_PEAK_INVALID",
		"AUDIO_VALIDATION_ERROR",
	},
	runtime: []string{
		"AUDIO_ENCODER_UNAVAILABLE",
		"AUDIO_RUNTIME_FFMPEG_NOT_FOUND",
		"AUDIO_RUNTIME_FFPROBE_NOT_FOUND",
		"AUDIO_RUNTIME_UNAVAILABLE",
	},
	unsupported: []string{
		"AUDIO_FORMAT_MISMATCH",
		"AUDIO_FORMAT_UNSUPPORTED",
		"AUDIO_INPUT_UNSUPPORTED",
	},
	cancelled: []string{
		"AUDIO_CANCELLED",
	},
	execution: []string{
		"AUDIO_EXECUTION_FAILED",
		"AUDIO_INPUT_OPEN_FAILED",
		"AUDIO_LOUDNORM_FAILED",
		"AUDIO_OUTPUT_WRITE_FAILED",
	},
}

// canonicalWebCodes classifies the URL downloader. The coarse WEB_DOWNLOAD_*
// bucket the engine returns stays as the job DetailCode; its family decides
// whether the failure is user-fixable, a missing dependency, unsupported, a
// cancellation or a transient execution failure.
var canonicalWebCodes = canonicalCodeFamilies{
	validation: []string{
		"WEB_DOWNLOAD_FORMAT_UNAVAILABLE",
		"WEB_DOWNLOAD_INVALID_URL",
		"WEB_DOWNLOAD_ITEMS_MISMATCH",
		"WEB_DOWNLOAD_ITEMS_REQUIRED",
		"WEB_DOWNLOAD_OPTIONS_INVALID",
	},
	runtime: []string{
		"WEB_DOWNLOAD_JS_RUNTIME_MISSING",
		"WEB_DOWNLOAD_RUNTIME_UNAVAILABLE",
	},
	unsupported: []string{
		"WEB_DOWNLOAD_UNSUPPORTED_SITE",
	},
	cancelled: []string{
		"WEB_DOWNLOAD_CANCELED",
	},
	execution: []string{
		"WEB_DOWNLOAD_EXECUTION_FAILED",
		"WEB_DOWNLOAD_METADATA_FAILED",
		"WEB_DOWNLOAD_NO_OUTPUT",
	},
}

// canonicalSystemCodes keeps runtime-wide codes (job engine, service layer)
// classified as they were before the per-domain table existed.
// canonicalRecipeCodes classifies the duplicate-and-edit errors so the UI and
// retry policy treat a bad override as a deterministic validation failure
// instead of an unknown or transient one.
var canonicalRecipeCodes = canonicalCodeFamilies{
	validation: []string{
		// An archive result that fails its own verification is deterministic:
		// retrying the same input cannot fix a missing PDF/A marker or a
		// checksum mismatch, so it must never be retried automatically.
		"RECIPE_ARCHIVE_VERIFICATION_FAILED",
		"RECIPE_BASE_NOT_FOUND",
		"RECIPE_NOT_DUPLICABLE",
		"RECIPE_OPTION_INVALID_TYPE",
		"RECIPE_OPTION_INVALID_VALUE",
		"RECIPE_OPTION_OUT_OF_RANGE",
		"RECIPE_OPTION_UNKNOWN",
		"RECIPE_USER_NAME_REQUIRED",
		"RECIPE_USER_NAME_TOO_LONG",
		"RECIPE_USER_NOT_FOUND",
	},
	execution: []string{
		"RECIPE_USER_DELETE_FAILED",
		"RECIPE_USER_SAVE_FAILED",
	},
}

var canonicalSystemCodes = canonicalCodeFamilies{
	validation: []string{
		// The headless CLI rejects an option the tool does not read instead of
		// silently ignoring a typo.
		"TOOL_OPTION_UNKNOWN",
		// The local MCP server reports a path outside the terminal-granted
		// allowlist and its per-call input cap as validation failures.
		"MCP_PATH_NOT_ALLOWED",
		"MCP_OUTPUT_NOT_ALLOWED",
		"MCP_INPUT_LIMIT_EXCEEDED",
	},
	runtime: []string{
		"RUNTIME_DEP_MISSING",
	},
	execution: []string{
		"EXECUTION_FAILED",
		"EXECUTION_ERROR",
		"PANIC",
	},
	unclassified: []string{
		// Lifecycle and internal consistency codes are not a domain failure and
		// have no honest canonical family.
		"JOB_INTERRUPTED",
		"PLAN_EXECUTION_MISMATCH",
	},
}

var canonicalCodeTable = buildCanonicalCodeTable(
	canonicalImageCodes,
	canonicalOutputNameCodes,
	canonicalPDFCodes,
	canonicalRecipeCodes,
	canonicalVideoCodes,
	canonicalAudioCodes,
	canonicalWebCodes,
	canonicalSystemCodes,
)

func buildCanonicalCodeTable(domains ...canonicalCodeFamilies) map[string]string {
	table := make(map[string]string)

	add := func(canonical string, codes []string) {
		for _, code := range codes {
			table[code] = canonical
		}
	}

	for _, domain := range domains {
		add(ErrorCodeValidationInvalidInput, domain.validation)
		add(ErrorCodeRuntimeDepMissing, domain.runtime)
		add(ErrorCodeUnsupportedFormat, domain.unsupported)
		add(ErrorCodeCancelledByUser, domain.cancelled)
		add(ErrorCodeExecIOTransient, domain.execution)
		add(ErrorCodeUnclassified, domain.unclassified)
	}

	return table
}

// CanonicalErrorCode maps a stable detail code to the canonical family the UI
// and retry policy reason about. The per-domain table is checked first so a
// deterministic failure never falls through to a transient family; the DOC
// keyword rules and the generic keyword rules stay as the fallback for codes
// that are not enumerated. A code that matches nothing is reported as
// ErrorCodeUnclassified instead of a transient failure.
func CanonicalErrorCode(detailCode string) string {
	code := strings.ToUpper(strings.TrimSpace(detailCode))

	if code == "" {
		return ErrorCodeUnclassified
	}

	if canonical, ok := canonicalCodeTable[code]; ok {
		return canonical
	}

	switch {
	case strings.Contains(code, "CANCELLED"), strings.Contains(code, "CANCELED"):
		return ErrorCodeCancelledByUser
	case strings.Contains(code, "TIMEOUT"):
		return ErrorCodeExecTimeoutTransient
	case strings.Contains(code, "DOC_DOCX_TO_PDF_RUNTIME"):
		return ErrorCodeRuntimeDepMissing
	case strings.Contains(code, "DOC_DOCX_TO_PDF_INPUT"), strings.Contains(code, "DOC_DOCX_TO_PDF_MODE"), strings.Contains(code, "DOC_DOCX_TO_PDF_OUTPUT"):
		return ErrorCodeValidationInvalidInput
	case strings.Contains(code, "DOC_DOCX_TO_PDF_FONTS"):
		return ErrorCodeExecIOTransient
	case strings.Contains(code, "DOC_DOCX_TO_PDF_PRIMARY_EXECUTION"), strings.Contains(code, "DOC_DOCX_TO_PDF_FALLBACK_EXECUTION"), strings.Contains(code, "DOC_DOCX_TO_PDF_EXECUTION"):
		return ErrorCodeExecIOTransient
	case strings.Contains(code, "UNSUPPORTED"), strings.Contains(code, "FORMAT_MISMATCH"):
		return ErrorCodeUnsupportedFormat
	case strings.Contains(code, "DOC_MD_TO_PDF_INPUT"), strings.Contains(code, "DOC_MD_TO_PDF_CONFIG"), strings.Contains(code, "DOC_MD_TO_PDF_ALIGN"), strings.Contains(code, "DOC_MD_TO_PDF_FONT"), strings.Contains(code, "DOC_MD_TO_PDF_COLOR"), strings.Contains(code, "DOC_MD_TO_PDF_MARGIN"), strings.Contains(code, "DOC_MD_TO_PDF_PLACEHOLDER"), strings.Contains(code, "DOC_MD_TO_PDF_MODE"), strings.Contains(code, "DOC_MD_TO_PDF_OUTPUT"):
		return ErrorCodeValidationInvalidInput
	case strings.Contains(code, "DOC_MD_TO_PDF_RENDER"):
		return ErrorCodeExecIOTransient
	case strings.Contains(code, "PDF_PREVIEW"):
		return ErrorCodeValidationInvalidInput
	case strings.Contains(code, "RUNTIME"), strings.Contains(code, "DEPENDENCY"), strings.Contains(code, "DEP_MISSING"):
		return ErrorCodeRuntimeDepMissing
	case strings.Contains(code, "VALIDATION"), strings.Contains(code, "INVALID"), strings.Contains(code, "NOT_FOUND"), strings.Contains(code, "MISSING"), strings.Contains(code, "PROTECTED"):
		return ErrorCodeValidationInvalidInput
	default:
		return ErrorCodeUnclassified
	}
}
