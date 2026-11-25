package execx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	helperEnv       = "EXECX_HELPER"
	helperModeEnv   = "EXECX_HELPER_MODE"
	helperCodeEnv   = "EXECX_HELPER_CODE"
	helperStderrEnv = "EXECX_HELPER_STDERR"
	helperLinesEnv  = "EXECX_HELPER_LINES"
	helperValueEnv  = "EXECX_HELPER_VALUE"
	helperChildEnv  = "EXECX_HELPER_CHILD"
)

// TestHelperProcess is not a real test: it is the body of the child processes
// started by the other tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("helper process only")
	}

	switch os.Getenv(helperModeEnv) {
	case "exit":
		fmt.Fprintln(os.Stderr, os.Getenv(helperStderrEnv))
		code, _ := strconv.Atoi(os.Getenv(helperCodeEnv))
		os.Exit(code)
	case "stdout-lines":
		count, _ := strconv.Atoi(os.Getenv(helperLinesEnv))
		for index := 0; index < count; index++ {
			fmt.Printf("line-%d\n", index)
		}
		os.Exit(0)
	case "stderr-lines":
		count, _ := strconv.Atoi(os.Getenv(helperLinesEnv))
		for index := 0; index < count; index++ {
			fmt.Fprintf(os.Stderr, "line-%d\n", index)
		}
		os.Exit(0)
	case "partial":
		fmt.Print("no-newline")
		os.Exit(0)
	case "huge-stdout":
		count, _ := strconv.Atoi(os.Getenv(helperLinesEnv))
		fmt.Print(strings.Repeat("a", count))
		os.Exit(0)
	case "huge-stderr":
		count, _ := strconv.Atoi(os.Getenv(helperLinesEnv))
		fmt.Fprint(os.Stderr, strings.Repeat("x", count))
		os.Exit(0)
	case "sleep":
		fmt.Println("started")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "cwd":
		dir, err := os.Getwd()
		if err != nil {
			os.Exit(2)
		}
		fmt.Println(dir)
		os.Exit(0)
	case "env":
		fmt.Println(os.Getenv(helperValueEnv))
		os.Exit(0)
	case "spawn-grandchild":
		// The Windows job-object test needs a process that spawns a long-lived
		// grandchild and then keeps running. The parent arms the job immediately
		// after CreateProcess; the short delay keeps that assignment strictly
		// earlier than the spawn so the test cannot race it.
		time.Sleep(250 * time.Millisecond)
		fields := strings.Fields(os.Getenv(helperChildEnv))
		if len(fields) == 0 {
			fmt.Fprintln(os.Stderr, "missing helper child command")
			os.Exit(3)
		}
		child := exec.Command(fields[0], fields[1:]...)
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		fmt.Printf("grandchild=%d\n", child.Process.Pid)
		time.Sleep(time.Minute)
		os.Exit(0)
	}

	os.Exit(2)
}

func TestRunCapturesExitCodeAndStderr(t *testing.T) {
	skipIfShort(t)

	result, err := Run(context.Background(), helperCommand("exit",
		helperCodeEnv+"=7",
		helperStderrEnv+"=boom happened",
	))
	if err == nil {
		t.Fatal("Run returned nil error for a failing command")
	}

	var commandErr *Error
	if !errors.As(err, &commandErr) {
		t.Fatalf("error type = %T, want *execx.Error", err)
	}
	if result.ExitCode != 7 || commandErr.ExitCode != 7 {
		t.Fatalf("exit code = %d/%d, want 7", result.ExitCode, commandErr.ExitCode)
	}
	if !strings.Contains(result.Stderr, "boom happened") {
		t.Fatalf("stderr = %q, want it to contain the failure output", result.Stderr)
	}
	if commandErr.Path != os.Args[0] {
		t.Fatalf("error path = %q, want %q", commandErr.Path, os.Args[0])
	}
	if commandErr.Unwrap() == nil {
		t.Fatal("Error.Unwrap returned nil for an exit failure")
	}
	if !strings.Contains(err.Error(), "exit code 7") || !strings.Contains(err.Error(), "boom happened") {
		t.Fatalf("error message = %q, want exit code and stderr", err.Error())
	}
}

func TestRunStreamsStdoutLines(t *testing.T) {
	skipIfShort(t)

	var lines []string
	result, err := Run(context.Background(), Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  helperEnvWith("stdout-lines", helperLinesEnv+"=3"),
		Stdout: func(line string) {
			lines = append(lines, line)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}

	want := []string{"line-0", "line-1", "line-2"}
	if strings.Join(lines, ",") != strings.Join(want, ",") {
		t.Fatalf("stdout lines = %v, want %v", lines, want)
	}
}

func TestRunEmitsTrailingPartialLine(t *testing.T) {
	skipIfShort(t)

	var lines []string
	_, err := Run(context.Background(), Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  helperEnvWith("partial"),
		Stdout: func(line string) {
			lines = append(lines, line)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(lines) != 1 || lines[0] != "no-newline" {
		t.Fatalf("stdout lines = %v, want [no-newline]", lines)
	}
}

func TestRunSplitsOversizedStdoutLines(t *testing.T) {
	skipIfShort(t)

	var lines []string
	_, err := Run(context.Background(), Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  helperEnvWith("huge-stdout", helperLinesEnv+"=200"),
		Stdout: func(line string) {
			lines = append(lines, line)
		},
		ScanMaxToken: 50,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	total := 0
	for _, line := range lines {
		total += len(line)
		if len(line) > 50 {
			t.Fatalf("line length = %d, want <= 50", len(line))
		}
	}
	if total != 200 {
		t.Fatalf("total stdout bytes = %d, want 200", total)
	}
	if len(lines) < 4 {
		t.Fatalf("line chunks = %d, want at least 4", len(lines))
	}
}

func TestRunBoundsStderrTail(t *testing.T) {
	skipIfShort(t)

	result, err := Run(context.Background(), Command{
		Path:            os.Args[0],
		Args:            []string{"-test.run=TestHelperProcess", "--"},
		Env:             helperEnvWith("stderr-lines", helperLinesEnv+"=20"),
		StderrTailLines: 3,
		StderrTailBytes: 128,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := strings.Split(result.Stderr, "\n")
	if len(lines) != 3 {
		t.Fatalf("stderr lines = %v, want 3 lines", lines)
	}
	if lines[0] != "line-17" || lines[2] != "line-19" {
		t.Fatalf("stderr tail = %v, want line-17..line-19", lines)
	}
}

func TestRunBoundsSingleHugeStderrLine(t *testing.T) {
	skipIfShort(t)

	result, err := Run(context.Background(), Command{
		Path:            os.Args[0],
		Args:            []string{"-test.run=TestHelperProcess", "--"},
		Env:             helperEnvWith("huge-stderr", helperLinesEnv+"=4096"),
		StderrTailBytes: 128,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Stderr) > 128 {
		t.Fatalf("stderr length = %d, want <= 128", len(result.Stderr))
	}
	if strings.Trim(result.Stderr, "x") != "" {
		t.Fatalf("stderr = %q, want only the x padding", result.Stderr)
	}
}

func TestRunUsesDefaultStderrTail(t *testing.T) {
	skipIfShort(t)

	result, err := Run(context.Background(), helperCommand("stderr-lines", helperLinesEnv+"=100"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := strings.Split(result.Stderr, "\n")
	if len(lines) != defaultStderrTailLines {
		t.Fatalf("stderr lines = %d, want %d", len(lines), defaultStderrTailLines)
	}
	if lines[len(lines)-1] != "line-99" {
		t.Fatalf("last stderr line = %q, want line-99", lines[len(lines)-1])
	}
}

func TestRunTimeoutKillsLongCommand(t *testing.T) {
	skipIfShort(t)

	started := time.Now()
	result, err := Run(context.Background(), Command{
		Path:    os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env:     helperEnvWith("sleep"),
		Timeout: 150 * time.Millisecond,
	})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Run returned nil error after the timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if result.ExitCode == 0 {
		t.Fatal("exit code = 0, want a killed process code")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %s, want it to return promptly after the timeout", elapsed)
	}
}

func TestRunHonorsContextCancellation(t *testing.T) {
	skipIfShort(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	_, err := Run(ctx, Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  helperEnvWith("sleep"),
	})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Run returned nil error after cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %s, want it to return promptly after cancellation", elapsed)
	}
}

func TestRunKillsProcessGroupOnUnix(t *testing.T) {
	skipIfShort(t)
	if runtime.GOOS == "windows" {
		t.Skip("process groups are Unix-specific")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh is not available")
	}

	started := time.Now()
	_, runErr := Run(context.Background(), Command{
		Path:    shell,
		Args:    []string{"-c", "sleep 30 & wait"},
		Timeout: 200 * time.Millisecond,
		Stdout:  func(string) {},
	})
	elapsed := time.Since(started)

	if runErr == nil {
		t.Fatal("Run returned nil error after the timeout")
	}
	if !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", runErr)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Run took %s, want a process-group kill instead of waiting on the orphan", elapsed)
	}
}

func TestRunUsesWorkingDirectory(t *testing.T) {
	skipIfShort(t)

	dir := t.TempDir()
	var lines []string
	_, err := Run(context.Background(), Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  helperEnvWith("cwd"),
		Dir:  dir,
		Stdout: func(line string) {
			lines = append(lines, line)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("stdout lines = %v, want one line", lines)
	}

	want := resolveSymlinks(t, dir)
	got := resolveSymlinks(t, lines[0])
	if got != want {
		t.Fatalf("working directory = %q, want %q", got, want)
	}
}

func TestRunReplacesEnvironment(t *testing.T) {
	skipIfShort(t)

	var lines []string
	_, err := Run(context.Background(), Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  helperEnvWith("env", helperValueEnv+"=hello-env"),
		Stdout: func(line string) {
			lines = append(lines, line)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(lines) != 1 || lines[0] != "hello-env" {
		t.Fatalf("stdout lines = %v, want [hello-env]", lines)
	}
}

func TestRunMissingBinaryReturnsError(t *testing.T) {
	result, err := Run(context.Background(), Command{Path: filepath.Join(t.TempDir(), "does-not-exist")})
	if err == nil {
		t.Fatal("Run returned nil error for a missing binary")
	}

	var commandErr *Error
	if !errors.As(err, &commandErr) {
		t.Fatalf("error type = %T, want *execx.Error", err)
	}
	if commandErr.ExitCode != -1 || result.ExitCode != -1 {
		t.Fatalf("exit code = %d/%d, want -1", result.ExitCode, commandErr.ExitCode)
	}
}

func TestRunRejectsEmptyPath(t *testing.T) {
	_, err := Run(context.Background(), Command{Path: "  "})
	if err == nil {
		t.Fatal("Run returned nil error for an empty path")
	}
	var commandErr *Error
	if !errors.As(err, &commandErr) || commandErr.ExitCode != -1 {
		t.Fatalf("error = %v, want an *execx.Error with exit code -1", err)
	}
}

func TestRunAcceptsNilContext(t *testing.T) {
	skipIfShort(t)

	// Run treats a nil context as context.Background.
	result, err := Run(nil, helperCommand("stdout-lines", helperLinesEnv+"=1"))
	if err != nil {
		t.Fatalf("Run with a nil context: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}
}

func helperCommand(mode string, extraEnv ...string) Command {
	return Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestHelperProcess", "--"},
		Env:  helperEnvWith(mode, extraEnv...),
	}
}

func helperEnvWith(mode string, extraEnv ...string) []string {
	// The child is race-instrumented when the suite runs with -race; the race
	// runtime otherwise sleeps one second at exit and inflates the suite time.
	env := append(os.Environ(), "GORACE=atexit_sleep_ms=0", helperEnv+"=1", helperModeEnv+"="+mode)

	return append(env, extraEnv...)
}

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("spawns subprocesses")
	}
}

func resolveSymlinks(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}

	return resolved
}
