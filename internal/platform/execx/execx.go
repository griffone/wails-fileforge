// Package execx runs external commands with bounded output, per-command
// timeouts, and reliable cancellation. On Unix it kills the whole process
// group so child engines (ffmpeg, LibreOffice, Ghostscript) cannot outlive a
// cancelled job.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	defaultStderrTailLines = 60
	defaultStderrTailBytes = 8 * 1024
	defaultScanMaxToken    = 1024 * 1024

	// commandWaitDelay bounds how long Wait blocks on inherited stdout/stderr
	// pipes after the direct process exits.
	commandWaitDelay = 5 * time.Second
)

// Command describes one external process invocation.
type Command struct {
	// Path is the executable to run.
	Path string
	// Args are the arguments passed to Path.
	Args []string
	// Dir is the working directory. Empty inherits the caller's directory.
	Dir string
	// Env replaces the process environment when non-nil.
	Env []string
	// Timeout bounds the whole command when greater than zero.
	Timeout time.Duration
	// Stdout receives stdout line by line when set. Lines are emitted without
	// their trailing newline; oversized lines are split into bounded chunks.
	Stdout func(line string)
	// StderrTailLines bounds the captured stderr tail. Zero uses the default.
	StderrTailLines int
	// StderrTailBytes bounds the captured stderr tail. Zero uses the default.
	StderrTailBytes int
	// ScanMaxToken bounds a single stdout/stderr line. Zero uses the default.
	ScanMaxToken int
}

// Result is the outcome of a finished command. Stderr holds only the bounded
// tail, not the complete output.
type Result struct {
	ExitCode int
	Stderr   string
}

// Error describes a command that failed to start or exited unsuccessfully.
type Error struct {
	Path     string
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

// Error returns a message with the cause, the command, its exit code, and the
// bounded stderr tail.
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	var message strings.Builder
	message.WriteString("execx: ")
	if e.Err != nil {
		message.WriteString(e.Err.Error())
	} else {
		message.WriteString("command failed")
	}
	if e.Path != "" {
		message.WriteString(": ")
		message.WriteString(e.Path)
		for _, arg := range e.Args {
			message.WriteByte(' ')
			message.WriteString(arg)
		}
	}
	if e.ExitCode >= 0 {
		fmt.Fprintf(&message, " (exit code %d)", e.ExitCode)
	}
	if e.Stderr != "" {
		message.WriteString(": ")
		message.WriteString(e.Stderr)
	}

	return message.String()
}

// Unwrap returns the underlying process or context error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Err
}

// Run executes cmd and waits for it to finish. A non-zero exit or a start
// failure returns an *Error; successful runs return a nil error.
func Run(ctx context.Context, cmd Command) (Result, error) {
	if strings.TrimSpace(cmd.Path) == "" {
		startErr := errors.New("execx: command path is required")
		return Result{ExitCode: -1}, &Error{ExitCode: -1, Err: startErr}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	runCtx := ctx
	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}

	process := exec.CommandContext(runCtx, cmd.Path, cmd.Args...)
	process.Dir = cmd.Dir
	if cmd.Env != nil {
		process.Env = cmd.Env
	}
	configureCommand(process)
	process.WaitDelay = commandWaitDelay

	stdout := newLineWriter(cmd.ScanMaxToken, cmd.Stdout)
	if cmd.Stdout != nil {
		process.Stdout = stdout
	}
	stderrTail := newTailBuffer(cmd.StderrTailLines, cmd.StderrTailBytes)
	stderr := newLineWriter(cmd.ScanMaxToken, stderrTail.add)
	process.Stderr = stderr

	if err := process.Start(); err != nil {
		cause := err
		if ctxErr := runCtx.Err(); ctxErr != nil {
			cause = fmt.Errorf("%w: %v", ctxErr, err)
		}
		return Result{ExitCode: -1}, &Error{
			Path:     cmd.Path,
			Args:     cmd.Args,
			ExitCode: -1,
			Err:      cause,
		}
	}

	waitErr := process.Wait()
	stdout.Flush()
	stderr.Flush()

	result := Result{Stderr: stderrTail.text()}
	if waitErr == nil {
		return result, nil
	}

	exitCode := -1
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	result.ExitCode = exitCode

	cause := waitErr
	if ctxErr := runCtx.Err(); ctxErr != nil {
		cause = fmt.Errorf("%w: %v", ctxErr, waitErr)
	}

	return result, &Error{
		Path:     cmd.Path,
		Args:     cmd.Args,
		ExitCode: exitCode,
		Stderr:   result.Stderr,
		Err:      cause,
	}
}

// lineWriter splits a byte stream into lines and hands each line to emit. It
// bounds memory by splitting lines longer than max into chunks.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	max  int
	emit func(string)
}

func newLineWriter(max int, emit func(string)) *lineWriter {
	if max <= 0 {
		max = defaultScanMaxToken
	}

	return &lineWriter{max: max, emit: emit}
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		index := bytes.IndexByte(w.buf, '\n')
		if index < 0 {
			break
		}
		w.emitLine(string(w.buf[:index]))
		w.buf = w.buf[index+1:]
	}
	for len(w.buf) > w.max {
		w.emitLine(string(w.buf[:w.max]))
		w.buf = w.buf[w.max:]
	}

	return len(p), nil
}

// Flush emits any trailing line that did not end with a newline.
func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for len(w.buf) > 0 {
		end := len(w.buf)
		if end > w.max {
			end = w.max
		}
		w.emitLine(string(w.buf[:end]))
		w.buf = w.buf[end:]
	}
	w.buf = nil
}

func (w *lineWriter) emitLine(line string) {
	if w.emit == nil {
		return
	}
	w.emit(strings.TrimSuffix(line, "\r"))
}

// tailBuffer keeps the last maxLines lines within maxBytes so a noisy engine
// cannot return megabytes of logs to the UI.
type tailBuffer struct {
	lines    []string
	size     int
	maxLines int
	maxBytes int
}

func newTailBuffer(maxLines, maxBytes int) *tailBuffer {
	if maxLines <= 0 {
		maxLines = defaultStderrTailLines
	}
	if maxBytes <= 0 {
		maxBytes = defaultStderrTailBytes
	}

	return &tailBuffer{maxLines: maxLines, maxBytes: maxBytes}
}

func (b *tailBuffer) add(line string) {
	if len(line) > b.maxBytes {
		line = line[len(line)-b.maxBytes:]
	}
	b.lines = append(b.lines, line)
	b.size += len(line) + 1
	for len(b.lines) > 1 && (len(b.lines) > b.maxLines || b.size > b.maxBytes) {
		b.size -= len(b.lines[0]) + 1
		b.lines = b.lines[1:]
	}
}

func (b *tailBuffer) text() string {
	joined := strings.Join(b.lines, "\n")
	if len(joined) > b.maxBytes {
		joined = joined[len(joined)-b.maxBytes:]
		if index := strings.IndexByte(joined, '\n'); index >= 0 {
			joined = joined[index+1:]
		}
	}

	return strings.TrimSpace(strings.ToValidUTF8(joined, ""))
}
