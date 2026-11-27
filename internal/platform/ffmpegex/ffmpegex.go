// Package ffmpegex runs the ffmpeg processes shared by every media domain.
// It owns the global flags, the process-tree-safe command runner, the bounded
// stderr tail, the machine-readable progress stream and the atomic publication
// of an output next to its destination.
//
// The video domain was the first consumer; the audio domain reuses the same
// runner instead of growing a second, subtly different implementation.
package ffmpegex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/platform/execx"
	"fileforge-desktop/internal/platform/fsx"
)

const (
	// StderrTailLines and StderrTailBytes bound the captured stderr tail so a
	// noisy ffmpeg run never returns megabytes of logs to the UI. Only the tail
	// is useful for a failure message.
	StderrTailLines = 80
	StderrTailBytes = 16 * 1024

	// CaptureStderrTailLines and CaptureStderrTailBytes bound the stderr tail
	// for runs that also need a machine-readable report from that stream, such
	// as loudnorm's JSON summary. They are larger on purpose: the report is
	// printed near the end of a verbose run.
	CaptureStderrTailLines = 400
	CaptureStderrTailBytes = 64 * 1024

	// tempFilePattern keeps every atomic output next to its destination.
	// os.CreateTemp replaces the "*" and preserves the final extension so
	// ffmpeg still infers the muxer from the file name.
	tempFilePattern = ".fileforge-*"
)

// GlobalArgs is prepended to every ffmpeg invocation: no banner, errors only,
// no periodic stats, and never read stdin. Encoding commands run inside a job
// engine and must never block waiting for a terminal.
var GlobalArgs = []string{"-hide_banner", "-loglevel", "error", "-nostats", "-nostdin"}

// WithGlobalArgs returns args with the global ffmpeg flags prepended.
func WithGlobalArgs(args []string) []string {
	combined := make([]string, 0, len(GlobalArgs)+len(args))
	combined = append(combined, GlobalArgs...)
	combined = append(combined, args...)

	return combined
}

// CommandRunner runs one external command. The production implementation kills
// the whole process group on cancellation and keeps only a bounded stderr tail.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string) error
}

// ExecCommandRunner runs real ffmpeg processes through execx so cancellation
// reaches every descendant, stdin is never inherited, and failure messages keep
// the historical "command failed: <cause> (<stderr>)" shape.
type ExecCommandRunner struct{}

// Run executes one command and discards its output on success.
func (r *ExecCommandRunner) Run(ctx context.Context, name string, args []string) error {
	result, err := execx.Run(ctx, execx.Command{
		Path:            name,
		Args:            args,
		StderrTailLines: StderrTailLines,
		StderrTailBytes: StderrTailBytes,
	})

	return CommandRunError(result, err)
}

// CommandRunError maps an execx failure onto the historical
// "command failed: <cause> (<stderr>)" shape shared by every runner method.
func CommandRunError(result execx.Result, err error) error {
	if err == nil {
		return nil
	}

	cause := err
	var execErr *execx.Error
	if errors.As(err, &execErr) && execErr.Err != nil {
		cause = execErr.Err
	}

	return fmt.Errorf("command failed: %w (%s)", cause, strings.TrimSpace(result.Stderr))
}

// TempOutputPath reserves a unique sibling of outputPath with the same
// extension. The placeholder is removed before returning so ffmpeg can create
// the file itself while the final extension still selects the muxer.
func TempOutputPath(outputPath string) (string, error) {
	trimmedPath := strings.TrimSpace(outputPath)
	if trimmedPath == "" {
		return "", errors.New("output path is required")
	}

	dir := filepath.Dir(trimmedPath)
	extension := filepath.Ext(trimmedPath)
	tempFile, err := os.CreateTemp(dir, tempFilePattern+extension)
	if err != nil {
		return "", err
	}
	tempPath := tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return "", closeErr
	}
	if removeErr := os.Remove(tempPath); removeErr != nil {
		return "", removeErr
	}

	return tempPath, nil
}

// PublishOutput creates the destination directory when needed and atomically
// replaces outputPath with the finished temp file.
func PublishOutput(tempPath, outputPath string) error {
	trimmedPath := strings.TrimSpace(outputPath)
	if trimmedPath == "" {
		return errors.New("output path is required")
	}
	if err := fsx.EnsureDir(filepath.Dir(trimmedPath), 0o755); err != nil {
		return err
	}

	return fsx.ReplaceFile(tempPath, trimmedPath)
}

// RemoveTempOutput removes a failed temp file; a missing file is not an error.
func RemoveTempOutput(tempPath string) {
	if strings.TrimSpace(tempPath) == "" {
		return
	}

	_ = os.Remove(tempPath)
}
