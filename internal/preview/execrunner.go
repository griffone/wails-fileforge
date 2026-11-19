package preview

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"time"
)

// maxCapturedProcessOutput bounds each captured stream so a noisy external tool
// cannot exhaust memory.
const maxCapturedProcessOutput = 16 * 1024 * 1024

// ExecRunner runs external commands with a context, returning stdout and stderr.
// Implementations should honour ctx cancellation. Keeping this as a small
// interface makes the preview processors testable.
type ExecRunner interface {
	// Run runs the command named by name with args. If stdin is non-nil, it will
	// be provided to the process's standard input. Returns stdout, stderr and
	// an error (if any). The implementation must respect ctx cancellation.
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, stderr []byte, err error)
}

// LocalExecRunner uses the local OS to execute commands. On Unix the child runs
// in its own process group so cancellation also kills its descendants.
type LocalExecRunner struct{}

func NewLocalExecRunner() ExecRunner { return &LocalExecRunner{} }

func (r *LocalExecRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.Command(name, args...)
	configureProcessGroup(cmd)
	// Wait must not block forever when a descendant keeps stdout/stderr open.
	cmd.WaitDelay = 2 * time.Second

	stdout := newCappedBuffer(maxCapturedProcessOutput)
	stderr := newCappedBuffer(maxCapturedProcessOutput)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return stdout.Bytes(), stderr.Bytes(), err
	case <-ctx.Done():
		// Kill the whole process group, then wait briefly for the pipes to
		// close. The caller observes ctx.Err() if the process refuses to die.
		_ = killProcess(cmd)
		select {
		case err := <-done:
			return stdout.Bytes(), stderr.Bytes(), err
		case <-time.After(2 * time.Second):
			return stdout.Bytes(), stderr.Bytes(), ctx.Err()
		}
	}
}

// cappedBuffer collects process output up to a fixed limit and silently drops
// the rest. It is safe for concurrent use by the exec copy goroutine and the
// reader that runs when a kill does not complete immediately.
type cappedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newCappedBuffer(limit int) *cappedBuffer {
	if limit <= 0 {
		limit = maxCapturedProcessOutput
	}

	return &cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)

	return len(p), nil
}

// Bytes returns a copy of the captured output.
func (b *cappedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]byte, b.buf.Len())
	copy(out, b.buf.Bytes())

	return out
}
