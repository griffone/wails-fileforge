//go:build !windows

package execx

import "os/exec"

// processTree is a no-op on Unix: configureCommand already starts the child in
// its own process group and replaces the cancel path with a group kill, so
// there is nothing to claim after start and nothing to release after wait.
type processTree struct{}

func newProcessTree() *processTree {
	return &processTree{}
}

func (t *processTree) configure(cmd *exec.Cmd) {
	configureCommand(cmd)
}

func (t *processTree) arm(*exec.Cmd) {}

func (t *processTree) close() {}
