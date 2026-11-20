//go:build !windows

package execx

import "os/exec"

func hideConsole(*exec.Cmd) {}
