//go:build windows

package execx

import "os/exec"

// configureCommand keeps the os/exec default: cancellation kills the direct
// process. Windows has no process-group equivalent with the same semantics.
func configureCommand(*exec.Cmd) {}
