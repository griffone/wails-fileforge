//go:build windows

package execx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestRunCancellationKillsGrandchildrenOnWindows proves the Job Object path:
// the direct child spawns a long-lived ping.exe, cancellation terminates the
// tree, and the grandchild is gone. Without the job only the direct child dies
// and the ping keeps running with its file handles open.
func TestRunCancellationKillsGrandchildrenOnWindows(t *testing.T) {
	skipIfShort(t)

	ping, err := exec.LookPath("ping.exe")
	if err != nil {
		t.Skipf("ping.exe is not available: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	openBefore := jobObjectsOpen.Load()
	grandchildren := make(chan int, 1)
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		_, runErr = Run(ctx, Command{
			Path: os.Args[0],
			Args: []string{"-test.run=TestHelperProcess", "--"},
			Env: helperEnvWith("spawn-grandchild",
				helperChildEnv+"="+ping+" -n 60 127.0.0.1",
			),
			Stdout: func(line string) {
				pid, ok := parseGrandchildPID(line)
				if !ok {
					return
				}
				select {
				case grandchildren <- pid:
				default:
				}
			},
		})
	}()

	var pid int
	select {
	case pid = <-grandchildren:
	case <-done:
		t.Fatalf("Run returned before reporting a grandchild: %v", runErr)
	case <-time.After(20 * time.Second):
		t.Fatal("the helper did not report a grandchild pid")
	}

	// Hold a handle to the exact process object before cancelling, so PID reuse
	// cannot turn the later wait into a false positive.
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatalf("OpenProcess(%d): %v", pid, err)
	}
	defer windows.CloseHandle(handle)

	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	if runErr == nil {
		t.Fatal("Run returned nil error after cancellation")
	}
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", runErr)
	}
	if open := jobObjectsOpen.Load(); open != openBefore {
		t.Fatalf("open job object handles = %d, want %d after a cancelled run", open, openBefore)
	}

	// WAIT_OBJECT_0 means the grandchild process object is signalled, i.e. the
	// process terminated; WAIT_TIMEOUT would mean it survived the cancellation.
	status, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil {
		t.Fatalf("WaitForSingleObject(%d): %v", pid, err)
	}
	if status != windows.WAIT_OBJECT_0 {
		t.Fatalf("grandchild %d still running after cancellation (wait status %#x)", pid, status)
	}

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		t.Fatalf("GetExitCodeProcess(%d): %v", pid, err)
	}
	if exitCode == 0 {
		t.Fatalf("grandchild %d exit code = 0, want a forced termination", pid)
	}
}

// TestRunClosesJobObjectOnNormalExit proves the happy path releases the job
// handle: repeated successful runs must not accumulate open job objects.
func TestRunClosesJobObjectOnNormalExit(t *testing.T) {
	skipIfShort(t)

	createdBefore := jobObjectsCreated.Load()
	openBefore := jobObjectsOpen.Load()

	const runs = 3
	for run := 0; run < runs; run++ {
		result, err := Run(context.Background(), helperCommand("stdout-lines", helperLinesEnv+"=1"))
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if result.ExitCode != 0 {
			t.Fatalf("run %d: exit code = %d, want 0", run, result.ExitCode)
		}
	}

	if created := jobObjectsCreated.Load() - createdBefore; created != runs {
		t.Fatalf("job objects created = %d, want %d", created, runs)
	}
	if open := jobObjectsOpen.Load(); open != openBefore {
		t.Fatalf("open job object handles = %d, want %d after %d normal exits", open, openBefore, runs)
	}
}

func parseGrandchildPID(line string) (int, bool) {
	value, ok := strings.CutPrefix(strings.TrimSpace(line), "grandchild=")
	if !ok {
		return 0, false
	}
	pid, err := strconv.Atoi(value)
	if err != nil || pid <= 0 {
		return 0, false
	}

	return pid, true
}
