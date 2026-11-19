//go:build windows

package preview

import (
	"errors"
	"os"
	"os/exec"
)

// configureProcessGroup keeps the default Windows process attributes: the
// platform has no process-group equivalent with the same semantics.
func configureProcessGroup(*exec.Cmd) {}

// killProcess kills the direct process.
func killProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := cmd.Process.Kill()
	if err == nil || errors.Is(err, os.ErrProcessDone) {
		return nil
	}

	return err
}
