//go:build windows

package execx

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Test seams. They count the job objects execx creates and the handles it still
// holds so a Windows test can prove the normal-exit path does not leak.
var (
	jobObjectsCreated atomic.Int64
	jobObjectsOpen    atomic.Int64
)

// processTree owns the Windows Job Object of one spawned process. Assigning the
// child right after start puts every process it spawns in the same job, so a
// single TerminateJobObject reaps the grandchildren (ffmpeg, LibreOffice,
// tesseract) that a direct-child kill leaves running with the output locked.
type processTree struct {
	mu        sync.Mutex
	job       windows.Handle
	cancelled bool
}

func newProcessTree() *processTree {
	return &processTree{}
}

// configure hides the child console and replaces os/exec's direct-child kill
// with a job-wide termination. The job handle only exists after arm, so a
// cancellation that wins the race against start falls back to killing the
// direct child, exactly as before.
func (t *processTree) configure(cmd *exec.Cmd) {
	hideConsole(cmd)

	cmd.Cancel = func() error {
		t.mu.Lock()
		t.cancelled = true
		job := t.job
		t.mu.Unlock()

		if job != 0 && terminateJobObject(job) {
			// TerminateJobObject also terminated the direct child, so the
			// fallback kill has nothing left to do.
			return nil
		}
		if cmd.Process == nil {
			return nil
		}
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return err
		}

		return nil
	}
}

// arm creates the job, applies JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and assigns
// the running process to it. Assignment happens as soon as the pid exists;
// children the process spawned before that instant are outside the job, which
// is the unavoidable gap between CreateProcess and the first chance to claim
// the process. Assignment is best effort: when Windows refuses it (for example
// an inherited job that forbids breakaway) the run keeps the direct-child kill
// and logs the reason instead of failing.
func (t *processTree) arm(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		slog.Warn("execx: create job object", "pid", pid, "error", err)
		return
	}
	jobObjectsCreated.Add(1)

	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		slog.Warn("execx: set job object limits", "pid", pid, "error", err)
		closeJobObject(job)
		return
	}

	// AssignProcessToJobObject requires PROCESS_SET_QUOTA and PROCESS_TERMINATE.
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		slog.Warn("execx: open process for job assignment", "pid", pid, "error", err)
		closeJobObject(job)
		return
	}
	assignErr := windows.AssignProcessToJobObject(job, process)
	windows.CloseHandle(process)
	if assignErr != nil {
		slog.Warn("execx: assign process to job object", "pid", pid, "error", assignErr)
		closeJobObject(job)
		return
	}

	t.mu.Lock()
	t.job = job
	cancelled := t.cancelled
	t.mu.Unlock()
	jobObjectsOpen.Add(1)

	if cancelled {
		// The context expired between start and assignment: the direct child was
		// already killed, but anything it spawned belongs to the job by now.
		terminateJobObject(job)
	}
}

// close terminates whatever is still in the job — a grandchild that outlived a
// direct child killed outside our control — and releases the handle, so a run
// that ends normally never leaves a job object behind.
func (t *processTree) close() {
	t.mu.Lock()
	job := t.job
	t.job = 0
	t.mu.Unlock()

	if job == 0 {
		return
	}

	terminateJobObject(job)
	closeJobObject(job)
	jobObjectsOpen.Add(-1)
}

// terminateJobObject kills every process still in the job. It reports whether
// the job was terminated so the caller can fall back to a direct-child kill.
func terminateJobObject(job windows.Handle) bool {
	if err := windows.TerminateJobObject(job, 1); err != nil {
		slog.Warn("execx: terminate job object", "error", err)
		return false
	}

	return true
}

func closeJobObject(job windows.Handle) {
	if err := windows.CloseHandle(job); err != nil {
		slog.Warn("execx: close job object handle", "error", err)
	}
}
