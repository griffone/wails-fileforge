package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"fileforge-desktop/internal/platform/execx"
)

// Canonical error codes for the URL downloader. They are the coarse bucket the
// tool maps onto its own contract; DetailCode carries the finer, stable reason.
const (
	CodeRuntimeUnavailable = "WEB_DOWNLOAD_RUNTIME_UNAVAILABLE"
	CodeInvalidURL         = "WEB_DOWNLOAD_INVALID_URL"
	CodeUnsupportedSite    = "WEB_DOWNLOAD_UNSUPPORTED_SITE"
	CodeMetadataFailed     = "WEB_DOWNLOAD_METADATA_FAILED"
	CodeFormatUnavailable  = "WEB_DOWNLOAD_FORMAT_UNAVAILABLE"
	CodeExecutionFailed    = "WEB_DOWNLOAD_EXECUTION_FAILED"
	CodeCanceled           = "WEB_DOWNLOAD_CANCELED"
	CodeNoOutput           = "WEB_DOWNLOAD_NO_OUTPUT"
	CodeJSRuntimeMissing   = "WEB_DOWNLOAD_JS_RUNTIME_MISSING"
)

// Detail codes. They are stable strings because the UI and the retry policy
// branch on them, so they live in one place instead of being inlined.
const (
	detailCanceled             = "canceled"
	detailProcessKilled        = "process_killed"
	detailRuntimeNotFound      = "yt_dlp_not_found"
	detailRuntimeProbeUnset    = "runtime_probe_not_configured"
	detailRuntimeCheckCancel   = "runtime_check_canceled"
	detailUnsupportedURL       = "unsupported_url"
	detailJSRuntimeMissing     = "javascript_runtime_missing"
	detailMetadataCommandFail  = "metadata_command_failed"
	detailMetadataInvalidJSON  = "invalid_metadata"
	detailDownloadCommandFail  = "download_command_failed"
	detailNoOutputPath         = "no_output_path"
	detailOutputMissing        = "output_missing"
	detailURLEmpty             = "url_required"
	detailURLInvalid           = "url_invalid"
	detailURLScheme            = "url_scheme_unsupported"
	detailURLHost              = "url_host_missing"
	detailOutputDirRequired    = "output_dir_required"
	detailOutputDirNotFound    = "output_dir_not_found"
	detailOutputDirNotDir      = "output_dir_not_directory"
	detailOutputDirNotWritable = "output_dir_not_writable"
	detailKindInvalid          = "kind_invalid"
	detailMaxHeightInvalid     = "max_height_invalid"
	detailBitrateInvalid       = "audio_bitrate_invalid"
)

// stderrDetailMaxBytes is a defensive cap for Details["stderr"]. execx already
// bounds the tail in production; this keeps fake runners and future call sites
// from leaking a whole log into the UI payload.
const stderrDetailMaxBytes = 8 * 1024

// WebError is the typed failure the downloader engine returns. Code is the
// canonical WEB_DOWNLOAD_* bucket, DetailCode the stable precise reason, and
// Details carries bounded diagnostic context (never a full log).
type WebError struct {
	Code       string
	DetailCode string
	Message    string
	Details    map[string]any
}

func (e *WebError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	if e.DetailCode == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}

	return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.DetailCode)
}

// classifyCommandError maps a failed yt-dlp run onto a WebError. fallbackCode is
// the bucket for an unclassified non-zero exit (metadata vs execution). The
// context is checked first so a cancellation is never reported as a site or
// format problem.
func classifyCommandError(ctx context.Context, err error, result execx.Result, fallbackCode, failureMessage string) *WebError {
	if err == nil {
		return nil
	}

	if ctx != nil && ctx.Err() != nil {
		return &WebError{
			Code:       CodeCanceled,
			DetailCode: detailCanceled,
			Message:    "the yt-dlp command was canceled",
			Details:    map[string]any{"cause": ctx.Err().Error()},
		}
	}

	if isRuntimeMissing(err) {
		return &WebError{
			Code:       CodeRuntimeUnavailable,
			DetailCode: detailRuntimeNotFound,
			Message:    "yt-dlp is not available; install it from the toolchain panel",
			Details:    map[string]any{"binary": toolBinary, "error": err.Error()},
		}
	}

	if isKilledProcess(err) {
		return &WebError{
			Code:       CodeCanceled,
			DetailCode: detailProcessKilled,
			Message:    "the yt-dlp process was stopped",
			Details:    map[string]any{"error": err.Error()},
		}
	}

	stderr := boundedStderr(result, err)
	lower := strings.ToLower(stderr)
	switch {
	case strings.Contains(lower, "unsupported url"):
		return &WebError{
			Code:       CodeUnsupportedSite,
			DetailCode: detailUnsupportedURL,
			Message:    "yt-dlp does not support this site or link",
			Details:    stderrDetails(stderr),
		}
	case strings.Contains(lower, "no supported javascript runtime"):
		return &WebError{
			Code:       CodeJSRuntimeMissing,
			DetailCode: detailJSRuntimeMissing,
			Message:    "yt-dlp needs a JavaScript runtime (Deno) to read this site; install Deno from the toolchain panel",
			Details:    stderrDetails(stderr),
		}
	}

	code := fallbackCode
	detail := detailMetadataCommandFail
	if fallbackCode == CodeExecutionFailed {
		detail = detailDownloadCommandFail
	}

	return &WebError{
		Code:       code,
		DetailCode: detail,
		Message:    failureMessage,
		Details:    runFailureDetails(result, err, stderr),
	}
}

// isRuntimeMissing reports a process that never started because the yt-dlp
// binary is absent (or cannot be executed).
func isRuntimeMissing(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}

	var execErr *execx.Error
	if errors.As(err, &execErr) && execErr.Path != "" && execErr.ExitCode < 0 {
		// execx sets Path only on a start failure; a wait failure leaves it
		// empty. A start failure caused by a missing or unexecutable binary is
		// a runtime problem.
		return true
	}

	return strings.Contains(strings.ToLower(err.Error()), "executable file not found")
}

// isKilledProcess reports a process that was terminated by a signal without the
// context being canceled: execx leaves Path empty and records exit code -1.
func isKilledProcess(err error) bool {
	var execErr *execx.Error
	if errors.As(err, &execErr) && execErr.Path == "" && execErr.ExitCode < 0 {
		return true
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "signal: killed") || strings.Contains(lower, "signal: terminated")
}

func boundedStderr(result execx.Result, err error) string {
	stderr := strings.TrimSpace(result.Stderr)
	if stderr == "" {
		var execErr *execx.Error
		if errors.As(err, &execErr) {
			stderr = strings.TrimSpace(execErr.Stderr)
		}
	}
	if len(stderr) > stderrDetailMaxBytes {
		stderr = stderr[len(stderr)-stderrDetailMaxBytes:]
		if index := strings.IndexByte(stderr, '\n'); index >= 0 {
			stderr = stderr[index+1:]
		}
	}

	return stderr
}

func stderrDetails(stderr string) map[string]any {
	if stderr == "" {
		return nil
	}

	return map[string]any{"stderr": stderr}
}

func runFailureDetails(result execx.Result, err error, stderr string) map[string]any {
	details := map[string]any{}
	if stderr != "" {
		details["stderr"] = stderr
	}

	exitCode := result.ExitCode
	var execErr *execx.Error
	if errors.As(err, &execErr) {
		exitCode = execErr.ExitCode
	}
	details["exitCode"] = exitCode

	return details
}
