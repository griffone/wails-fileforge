package execx

import (
	"context"
	"os/exec"
)

// CommandContext builds an external command whose console window is hidden on
// Windows. FileForge is a GUI application; every child process it starts
// (ffmpeg, tesseract, python, micromamba, Ghostscript, ...) is a console
// program and must never flash a console window at the user.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	HideConsole(command)

	return command
}

// NewCommand is the non-context variant of CommandContext.
func NewCommand(name string, args ...string) *exec.Cmd {
	command := exec.Command(name, args...)
	HideConsole(command)

	return command
}

// HideConsole suppresses the child console window on Windows. It is a no-op on
// other platforms.
func HideConsole(command *exec.Cmd) {
	hideConsole(command)
}
