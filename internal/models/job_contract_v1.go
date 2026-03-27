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
)

func NewJobError(code, detailCode, message string, details map[string]any) *JobErrorV1 {
	err := &JobErrorV1{
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

func NewCanonicalJobError(detailCode, message string, details map[string]any) *JobErrorV1 {
	return NewJobError(CanonicalErrorCode(detailCode), detailCode, message, details)
}

func CanonicalErrorCode(detailCode string) string {
	code := strings.ToUpper(strings.TrimSpace(detailCode))

	switch {
	case code == "":
		return ErrorCodeExecIOTransient
	case strings.Contains(code, "CANCELLED"), strings.Contains(code, "CANCELED"):
		return ErrorCodeCancelledByUser
	case strings.Contains(code, "TIMEOUT"):
		return ErrorCodeExecTimeoutTransient
	case strings.Contains(code, "UNSUPPORTED"), strings.Contains(code, "FORMAT_MISMATCH"):
		return ErrorCodeUnsupportedFormat
	case strings.Contains(code, "PDF_PREVIEW"):
		return ErrorCodeValidationInvalidInput
	case strings.Contains(code, "RUNTIME"), strings.Contains(code, "DEPENDENCY"), strings.Contains(code, "DEP_MISSING"):
		return ErrorCodeRuntimeDepMissing
	case strings.Contains(code, "VALIDATION"), strings.Contains(code, "INVALID"), strings.Contains(code, "NOT_FOUND"), strings.Contains(code, "MISSING"):
		return ErrorCodeValidationInvalidInput
	default:
		return ErrorCodeExecIOTransient
	}
}
