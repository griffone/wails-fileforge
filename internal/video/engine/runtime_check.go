package engine

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

const ErrorCodeRuntimeUnavailable = "VIDEO_RUNTIME_UNAVAILABLE"

const (
	ErrorCodeRuntimeFFmpegNotFound  = "VIDEO_RUNTIME_FFMPEG_NOT_FOUND"
	ErrorCodeRuntimeFFprobeNotFound = "VIDEO_RUNTIME_FFPROBE_NOT_FOUND"
)

var ErrFFmpegNotFound = errors.New("ffmpeg runtime unavailable")

type RuntimeProbe interface {
	Check(ctx context.Context) error
}

type FFmpegRuntimeProbe struct {
	lookupPath func(file string) (string, error)
}

func NewFFmpegRuntimeProbe() *FFmpegRuntimeProbe {
	return &FFmpegRuntimeProbe{lookupPath: exec.LookPath}
}

func (p *FFmpegRuntimeProbe) Check(ctx context.Context) error {
	if p == nil {
		return &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: "video runtime probe is not configured", Cause: ErrFFmpegNotFound}
	}

	select {
	case <-ctx.Done():
		return &VideoError{Code: "CANCELED", Message: "runtime check canceled", Cause: ctx.Err()}
	default:
	}

	lookup := p.lookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}

	if _, err := lookup("ffmpeg"); err != nil {
		return &VideoError{Code: ErrorCodeRuntimeFFmpegNotFound, Message: "ffmpeg binary was not found in PATH", Cause: fmt.Errorf("ffmpeg lookup failed: %w", err)}
	}

	if _, err := lookup("ffprobe"); err != nil {
		return &VideoError{Code: ErrorCodeRuntimeFFprobeNotFound, Message: "ffprobe binary was not found in PATH", Cause: fmt.Errorf("ffprobe lookup failed: %w", err)}
	}

	return nil
}
