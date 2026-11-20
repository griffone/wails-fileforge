//go:build windows

package execx

import "os/exec"

// configureCommand keeps the os/exec default for cancellation (the direct
// process is killed) and hides the child console window: a GUI app must never
// flash a console for ffmpeg, tesseract, python or the toolchain installer.
func configureCommand(cmd *exec.Cmd) {
	hideConsole(cmd)
}
