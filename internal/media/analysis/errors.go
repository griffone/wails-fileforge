package analysis

import (
	"fmt"
)

// Canonical detail codes. They are stable strings because the sync tool, the
// Wails service and the retry policy branch on them, so they live in one place
// instead of being inlined at the call sites.
const (
	// CodeRuntimeUnavailable means the ffmpeg binary could not be started.
	CodeRuntimeUnavailable = "ANALYSIS_RUNTIME_UNAVAILABLE"
	// CodeDecodeFailed means ffmpeg ran but could not decode the audio.
	CodeDecodeFailed = "ANALYSIS_DECODE_FAILED"
	// CodeNoAudio means the file has no audio stream at all.
	CodeNoAudio = "ANALYSIS_NO_AUDIO"
	// CodeTooLong means a decoded file exceeded MaxDecodeHours.
	CodeTooLong = "ANALYSIS_TOO_LONG"
	// CodeCanceled means the context ended before the work finished.
	CodeCanceled = "ANALYSIS_CANCELED"
	// CodeInputInvalid means a path or a numeric option is unusable.
	CodeInputInvalid = "ANALYSIS_INPUT_INVALID"
	// CodeInsufficientOverlap means the two files share too little audio to
	// measure an offset.
	CodeInsufficientOverlap = "ANALYSIS_INSUFFICIENT_OVERLAP"
)

// Error is a typed failure from the analysis package. Code is the stable detail
// code callers classify on; Cause keeps the wrapped process or OS error for the
// logs and for errors.Is/errors.As.
type Error struct {
	Code    string
	Message string
	Cause   error
}

// Error renders the code, the message and the cause when one exists.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}

	return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
}

// Unwrap returns the underlying cause so callers can inspect it.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}
