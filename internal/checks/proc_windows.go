//go:build windows

package checks

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

var shell = []string{"cmd", "/d", "/s", "/c"}

// tree puts the check in a Job Object created with KILL_ON_JOB_CLOSE, so
// terminating or closing the job ends everything the check started.
//
// os/exec can't start a process suspended, so the shell is assigned to the job
// just after Start. Anything it spawns in that window escapes the job; in
// practice cmd hasn't parsed its command line yet.
type tree struct {
	job windows.Handle // 0 when the job couldn't be made; kill falls back to the shell
}

func newTree(*exec.Cmd) *tree {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return &tree{}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return &tree{}
	}
	return &tree{job: job}
}

// started assigns the running shell to the job.
func (t *tree) started(cmd *exec.Cmd) {
	if t.job == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(t.job, h)
}

// kill terminates the job, and the shell directly in case it never joined.
func (t *tree) kill(cmd *exec.Cmd) error {
	if t.job != 0 {
		_ = windows.TerminateJobObject(t.job, 1)
	}
	return cmd.Process.Kill()
}

// close releases the job; KILL_ON_JOB_CLOSE ends anything still in it.
func (t *tree) close() {
	if t.job != 0 {
		windows.CloseHandle(t.job)
		t.job = 0
	}
}
