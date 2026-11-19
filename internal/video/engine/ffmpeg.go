package engine

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
	// Bounded stderr prevents a noisy ffmpeg run from returning megabytes of
	// logs to the UI. Only the tail is useful for a failure message.
	ffmpegStderrTailLines = 80
	ffmpegStderrTailBytes = 16 * 1024

	// videoTempFilePattern keeps every atomic output next to its destination.
	// os.CreateTemp replaces the "*" and preserves the final extension so
	// ffmpeg still infers the muxer from the file name.
	videoTempFilePattern = ".fileforge-*"
)

// ffmpegGlobalArgs is prepended to every ffmpeg invocation: no banner, errors
// only, no periodic stats, and never read stdin. Encoding commands run inside a
// job engine and must never block waiting for a terminal.
var ffmpegGlobalArgs = []string{"-hide_banner", "-loglevel", "error", "-nostats", "-nostdin"}

// withFFmpegGlobalArgs returns args with the global ffmpeg flags prepended.
func withFFmpegGlobalArgs(args []string) []string {
	combined := make([]string, 0, len(ffmpegGlobalArgs)+len(args))
	combined = append(combined, ffmpegGlobalArgs...)
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

func (r *ExecCommandRunner) Run(ctx context.Context, name string, args []string) error {
	result, err := execx.Run(ctx, execx.Command{
		Path:            name,
		Args:            args,
		StderrTailLines: ffmpegStderrTailLines,
		StderrTailBytes: ffmpegStderrTailBytes,
	})
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

// tempOutputPath reserves a unique sibling of outputPath with the same
// extension. The placeholder is removed before returning so ffmpeg can create
// the file itself while the final extension still selects the muxer.
func tempOutputPath(outputPath string) (string, error) {
	trimmedPath := strings.TrimSpace(outputPath)
	if trimmedPath == "" {
		return "", errors.New("video: output path is required")
	}

	dir := filepath.Dir(trimmedPath)
	extension := filepath.Ext(trimmedPath)
	tempFile, err := os.CreateTemp(dir, videoTempFilePattern+extension)
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

// publishOutput creates the destination directory when needed and atomically
// replaces outputPath with the finished temp file.
func publishOutput(tempPath, outputPath string) error {
	trimmedPath := strings.TrimSpace(outputPath)
	if trimmedPath == "" {
		return errors.New("video: output path is required")
	}
	if err := fsx.EnsureDir(filepath.Dir(trimmedPath), 0o755); err != nil {
		return err
	}

	return fsx.ReplaceFile(tempPath, trimmedPath)
}

// removeTempOutput removes a failed temp file; a missing file is not an error.
func removeTempOutput(tempPath string) {
	if strings.TrimSpace(tempPath) == "" {
		return
	}

	_ = os.Remove(tempPath)
}
