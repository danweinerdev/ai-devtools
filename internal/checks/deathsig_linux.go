//go:build linux

package checks

import "syscall"

// setDeathSignal makes the kernel SIGKILL the check's shell if aid dies
// without cleaning up, e.g. from a SIGKILL that no handler can catch. It
// reaches the direct child only; aid's signal handling, which cancels the
// context and kills the whole group, covers the rest of the tree whenever aid
// gets the chance to run it.
func setDeathSignal(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}
