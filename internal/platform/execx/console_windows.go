//go:build windows

package execx

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW. HideWindow alone is not always enough
// for a console-subsystem child launched from a windowsgui parent; the flag
// prevents Windows from allocating a console for the process.
const createNoWindow = 0x08000000

func hideConsole(command *exec.Cmd) {
	if command == nil {
		return
	}
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}

	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNoWindow
}
