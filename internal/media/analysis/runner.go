package analysis

import (
	"context"

	"fileforge-desktop/internal/platform/execx"
	"fileforge-desktop/internal/platform/ffmpegex"
)

// Runner executes one external command. The returned stderr is the bounded tail
// of the process output, available on success and failure, so callers can
// classify a decode problem without a second probe. The interface is
// intentionally thin: the engine only needs a process boundary it can fake.
type Runner interface {
	Run(ctx context.Context, path string, args []string) (stderr string, err error)
}

// ExecRunner runs real processes through execx, which kills the whole process
// tree on cancellation and never lets a noisy ffmpeg return megabytes of logs.
type ExecRunner struct{}

// NewExecRunner returns the production runner.
func NewExecRunner() Runner {
	return ExecRunner{}
}

// Run executes one command and returns the bounded stderr tail. The error is
// the original execx error (when the command was started) so the caller can
// tell a missing binary from a failed decode.
func (ExecRunner) Run(ctx context.Context, path string, args []string) (string, error) {
	result, err := execx.Run(ctx, execx.Command{
		Path:            path,
		Args:            args,
		StderrTailLines: ffmpegex.StderrTailLines,
		StderrTailBytes: ffmpegex.StderrTailBytes,
	})

	return result.Stderr, err
}
