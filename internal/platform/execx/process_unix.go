//go:build !windows

package execx

import (
	"errors"
	"os/exec"
	"syscall"
)

// configureCommand starts the child in its own process group and replaces the
// default cancel behavior with a process-group kill.
func configureCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == nil || errors.Is(err, syscall.ESRCH) {
			return nil
		}

		return err
	}
}
