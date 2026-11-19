//go:build !windows

package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLocalExecRunnerKillsProcessOnCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires an external shell and process signals")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, _, err := NewLocalExecRunner().Run(ctx, "sh", []string{"-c", "sleep 30"}, nil)
	if err == nil {
		t.Fatal("expected the canceled command to fail")
	}
	if ctx.Err() == nil {
		t.Fatal("expected the context to be done")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("expected cancellation to be prompt, took %s", elapsed)
	}
}

func TestLocalExecRunnerKillsProcessGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("requires an external shell and process signals")
	}

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	script := `sleep 30 & echo $! > "$1"; wait`

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	_, _, err := NewLocalExecRunner().Run(ctx, "sh", []string{"-c", script, "preview-test", pidFile}, nil)
	if err == nil {
		t.Fatal("expected the canceled command to fail")
	}

	rawPID, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("read child pid: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if parseErr != nil {
		t.Fatalf("parse child pid %q: %v", rawPID, parseErr)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("child process %d survived process-group cancellation", pid)
}
