package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// RuntimeProbe checks that the process-level ffmpeg runtime is usable.
type RuntimeProbe interface {
	Check(ctx context.Context) error
}

// FFmpegRuntimeProbe verifies that ffmpeg and ffprobe are reachable. ffprobe is
// required because verification and progress reporting read media metadata.
type FFmpegRuntimeProbe struct {
	lookupPath func(file string) (string, error)
}

// NewFFmpegRuntimeProbe builds the production probe.
func NewFFmpegRuntimeProbe() *FFmpegRuntimeProbe {
	return &FFmpegRuntimeProbe{lookupPath: exec.LookPath}
}

// Check reports a canonical dependency error when either binary is missing.
func (p *FFmpegRuntimeProbe) Check(ctx context.Context) error {
	if p == nil {
		return &AudioError{Code: ErrorCodeRuntimeUnavailable, Message: "audio runtime probe is not configured"}
	}

	select {
	case <-ctx.Done():
		return &AudioError{Code: ErrorCodeCancelled, Message: "runtime check canceled", Cause: ctx.Err()}
	default:
	}

	lookup := p.lookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}

	if _, err := lookup("ffmpeg"); err != nil {
		return &AudioError{
			Code:    ErrorCodeRuntimeFFmpegNotFound,
			Message: "ffmpeg binary was not found in PATH; install ffmpeg to enable the audio tools",
			Cause:   fmt.Errorf("ffmpeg lookup failed: %w", err),
		}
	}

	if _, err := lookup("ffprobe"); err != nil {
		return &AudioError{
			Code:    ErrorCodeRuntimeFFprobeNotFound,
			Message: "ffprobe binary was not found in PATH; install ffprobe (usually with ffmpeg) to enable the audio tools",
			Cause:   fmt.Errorf("ffprobe lookup failed: %w", err),
		}
	}

	return nil
}

func resolveRuntimeProbe(probe RuntimeProbe) RuntimeProbe {
	if probe != nil {
		return probe
	}

	return NewFFmpegRuntimeProbe()
}

// runtimeErrorCode maps an arbitrary probe failure onto a canonical audio
// dependency code.
func runtimeErrorCode(err error) string {
	var audioErr *AudioError
	if errors.As(err, &audioErr) && audioErr != nil {
		switch audioErr.Code {
		case ErrorCodeRuntimeFFmpegNotFound, ErrorCodeRuntimeFFprobeNotFound, ErrorCodeRuntimeUnavailable:
			return audioErr.Code
		}
	}

	return ErrorCodeRuntimeUnavailable
}
