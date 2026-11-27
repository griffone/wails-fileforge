package engine

import (
	"context"
	"slices"

	"fileforge-desktop/internal/platform/execx"
)

const (
	// stderrTailLines and stderrTailBytes bound the captured stderr so a noisy
	// run never returns more than a short tail to the UI.
	stderrTailLines = 80
	stderrTailBytes = 16 * 1024

	// metadataMaxLineBytes allows a full `yt-dlp -J` document on one line.
	// execx splits lines above the limit, which would corrupt the JSON, so the
	// probe gets a generous ceiling.
	metadataMaxLineBytes = 64 * 1024 * 1024
	defaultMaxLineBytes  = 1024 * 1024
)

// CommandRunner runs one yt-dlp invocation. Stdout receives the command's
// stdout line by line and may be nil. The interface is intentionally thin: the
// engine only needs a process boundary it can fake in tests.
type CommandRunner interface {
	Run(ctx context.Context, path string, args []string, stdout func(line string)) (execx.Result, error)
}

// ExecCommandRunner runs real yt-dlp processes through execx, which kills the
// whole process tree on cancellation and keeps a bounded stderr tail.
type ExecCommandRunner struct{}

// Run executes one yt-dlp command.
func (r *ExecCommandRunner) Run(ctx context.Context, path string, args []string, stdout func(line string)) (execx.Result, error) {
	return execx.Run(ctx, execx.Command{
		Path:            path,
		Args:            args,
		Stdout:          stdout,
		StderrTailLines: stderrTailLines,
		StderrTailBytes: stderrTailBytes,
		ScanMaxToken:    scanMaxToken(args),
	})
}

// scanMaxToken gives metadata dumps room for a single long JSON line while
// keeping ordinary progress streams at the execx default.
func scanMaxToken(args []string) int {
	if slices.Contains(args, "-J") {
		return metadataMaxLineBytes
	}

	return defaultMaxLineBytes
}
