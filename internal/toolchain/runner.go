package toolchain

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner runs an external command and returns its combined output.
// stdout and stderr are merged so a failed command can report the tool message.
type CommandRunner interface {
	Run(ctx context.Context, name string, args []string, env []string) ([]byte, error)
}

// ExecCommandRunner runs real binaries with exec.CommandContext, so
// cancellation kills micromamba or a version probe.
type ExecCommandRunner struct{}

// Run implements CommandRunner.
func (r *ExecCommandRunner) Run(ctx context.Context, name string, args []string, env []string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		command.Env = env
	}

	output, err := command.CombinedOutput()
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return output, ctx.Err()
		}

		message := strings.TrimSpace(string(output))
		if message == "" {
			return output, fmt.Errorf("%s failed: %w", name, err)
		}

		return output, fmt.Errorf("%s failed: %w: %s", name, err, message)
	}

	return output, nil
}
