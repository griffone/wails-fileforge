package engine

import "fmt"

// Canonical audio detail codes. Every code maps to a canonical family in
// models.job_contract.go; validation, unsupported-format, missing-dependency and
// cancellation codes are deliberately excluded from the retry allowlist.
const (
	ErrorCodeValidation             = "AUDIO_VALIDATION_ERROR"
	ErrorCodeInputRequired          = "AUDIO_INPUT_REQUIRED"
	ErrorCodeInputNotFound          = "AUDIO_INPUT_NOT_FOUND"
	ErrorCodeInputUnsupported       = "AUDIO_INPUT_UNSUPPORTED"
	ErrorCodeFormatUnsupported      = "AUDIO_FORMAT_UNSUPPORTED"
	ErrorCodeFormatMismatch         = "AUDIO_FORMAT_MISMATCH"
	ErrorCodeBitrateInvalid         = "AUDIO_BITRATE_INVALID"
	ErrorCodeTargetLufsInvalid      = "AUDIO_TARGET_LUFS_INVALID"
	ErrorCodeTruePeakInvalid        = "AUDIO_TRUE_PEAK_INVALID"
	ErrorCodeOutputExists           = "AUDIO_OUTPUT_EXISTS"
	ErrorCodeOutputCollidesInput    = "AUDIO_OUTPUT_COLLIDES_INPUT"
	ErrorCodeOutputDirNotFound      = "AUDIO_OUTPUT_DIR_NOT_FOUND"
	ErrorCodeOutputDirNotDirectory  = "AUDIO_OUTPUT_DIR_NOT_DIRECTORY"
	ErrorCodeOutputDirNotWritable   = "AUDIO_OUTPUT_DIR_NOT_WRITABLE"
	ErrorCodeEncoderUnavailable     = "AUDIO_ENCODER_UNAVAILABLE"
	ErrorCodeRuntimeUnavailable     = "AUDIO_RUNTIME_UNAVAILABLE"
	ErrorCodeRuntimeFFmpegNotFound  = "AUDIO_RUNTIME_FFMPEG_NOT_FOUND"
	ErrorCodeRuntimeFFprobeNotFound = "AUDIO_RUNTIME_FFPROBE_NOT_FOUND"
	ErrorCodeExecutionFailed        = "AUDIO_EXECUTION_FAILED"
	ErrorCodeInputOpenFailed        = "AUDIO_INPUT_OPEN_FAILED"
	ErrorCodeOutputWriteFailed      = "AUDIO_OUTPUT_WRITE_FAILED"
	ErrorCodeLoudnormFailed         = "AUDIO_LOUDNORM_FAILED"
	ErrorCodeCancelled              = "AUDIO_CANCELLED"
)

// AudioError is a typed failure from the audio engine. Code is a stable detail
// code callers can classify; Cause keeps the wrapped process error for logs.
type AudioError struct {
	Code    string
	Message string
	Cause   error
}

func (e *AudioError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *AudioError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
