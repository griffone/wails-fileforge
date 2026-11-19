//go:build !windows

package preview

import (
	"errors"
	"os/exec"
	"syscall"
)

// configureProcessGroup starts the child in its own process group so a
// cancellation can signal descendants as well.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcess kills the direct process and every descendant in its group.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}

	return err
}
