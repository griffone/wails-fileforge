package engine

import (
	"context"
	"os/exec"
)

// RuntimeProbe reports whether the downloader runtime is usable. It only checks
// binary presence; format and site support are decided by an actual probe.
type RuntimeProbe interface {
	Check(ctx context.Context) error
}

// ToolRuntimeProbe looks up the yt-dlp binary in PATH.
type ToolRuntimeProbe struct {
	binary     string
	lookupPath func(string) (string, error)
}

// NewToolRuntimeProbe resolves yt-dlp through PATH. No version check is made:
// the tool plan decides whether the managed or the system binary is used.
func NewToolRuntimeProbe() RuntimeProbe {
	return &ToolRuntimeProbe{binary: toolBinary, lookupPath: exec.LookPath}
}

// Check reports CodeRuntimeUnavailable when the binary is missing and
// CodeCanceled when the context is already done.
func (p *ToolRuntimeProbe) Check(ctx context.Context) error {
	if p == nil {
		return &WebError{
			Code:       CodeRuntimeUnavailable,
			DetailCode: detailRuntimeProbeUnset,
			Message:    "the downloader runtime probe is not configured",
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-ctx.Done():
		return &WebError{
			Code:       CodeCanceled,
			DetailCode: detailRuntimeCheckCancel,
			Message:    "the runtime check was canceled",
			Details:    map[string]any{"cause": ctx.Err().Error()},
		}
	default:
	}

	lookup := p.lookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	binary := p.binary
	if binary == "" {
		binary = toolBinary
	}

	if _, err := lookup(binary); err != nil {
		return &WebError{
			Code:       CodeRuntimeUnavailable,
			DetailCode: detailRuntimeNotFound,
			Message:    "yt-dlp is not available; install it from the toolchain panel",
			Details:    map[string]any{"binary": binary, "error": err.Error()},
		}
	}

	return nil
}
