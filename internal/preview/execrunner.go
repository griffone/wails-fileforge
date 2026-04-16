package preview

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"
)

// ExecRunner runs external commands with a context, returning stdout and stderr.
// Implementations should honour ctx cancellation. Keeping this as a small
// interface makes the preview processors testable.
type ExecRunner interface {
	// Run runs the command named by name with args. If stdin is non-nil, it will
	// be provided to the process's standard input. Returns stdout, stderr and
	// an error (if any). The implementation must respect ctx cancellation.
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, stderr []byte, err error)
}

// LocalExecRunner uses the local OS to execute commands via exec.CommandContext.
// TODO: implement RLIMIT setup for subprocesses when required — keep code
// structure ready to add resource limit handling.
type LocalExecRunner struct{}

func NewLocalExecRunner() ExecRunner { return &LocalExecRunner{} }

func (r *LocalExecRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	// Set minimal SysProcAttr for potential RLIMIT additions later
	cmd.SysProcAttr = &syscall.SysProcAttr{}

	// Run in goroutine so we can react to ctx cancellation reliably
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()
	select {
	case err := <-done:
		return stdout.Bytes(), stderr.Bytes(), err
	case <-ctx.Done():
		// Attempt graceful kill, then force
		_ = cmd.Process.Kill()
		select {
		case err := <-done:
			return stdout.Bytes(), stderr.Bytes(), err
		case <-time.After(2 * time.Second):
			return stdout.Bytes(), stderr.Bytes(), ctx.Err()
		}
	}
}
