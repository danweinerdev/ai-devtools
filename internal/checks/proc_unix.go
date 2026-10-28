//go:build !windows

package checks

import (
	"os/exec"
	"syscall"
)

var shell = []string{"sh", "-c"}

// tree starts the check in its own process group, so kill reaches everything
// it spawned.
type tree struct{ pgid int }

func newTree(cmd *exec.Cmd) *tree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setDeathSignal(cmd.SysProcAttr)
	return &tree{}
}

func (t *tree) started(cmd *exec.Cmd) { t.pgid = cmd.Process.Pid }

// kill kills the check's whole process group.
func (*tree) kill(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// close kills whatever the check left running once it has finished, passing
// or not, matching the Windows job object: a check is one run and leaves no
// processes behind.
func (t *tree) close() {
	if t.pgid > 0 {
		_ = syscall.Kill(-t.pgid, syscall.SIGKILL)
	}
}
