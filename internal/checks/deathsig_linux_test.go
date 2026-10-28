//go:build linux

package checks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a helper process that runs one long check, so a test
// can SIGKILL it mid-check.
func TestMain(m *testing.M) {
	if dir := os.Getenv("AID_CHECKS_HELPER_DIR"); dir != "" {
		// The shell records its PID and execs sleep, so the PID is the check.
		Run(context.Background(), dir, "echo $$ > pid; exec sleep 30", time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSIGKILLedRunnerTakesItsCheckWithIt(t *testing.T) {
	dir := t.TempDir()
	helper := exec.Command(os.Args[0], "-test.run=^$")
	helper.Env = append(os.Environ(), "AID_CHECKS_HELPER_DIR="+dir)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 {
		if b, err := os.ReadFile(filepath.Join(dir, "pid")); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		if time.Now().After(deadline) {
			helper.Process.Kill()
			t.Fatal("the helper's check never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !alive(pid) {
		t.Fatal("the check isn't running before the kill")
	}
	helper.Process.Kill() // SIGKILL: no handler, no deferred cleanup
	helper.Wait()
	deadline = time.Now().Add(3 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			syscallKill(pid)
			t.Fatal("the check outlived its SIGKILLed runner")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// alive reports a running process; a zombie awaiting its reaper counts as gone.
func alive(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || i+2 >= len(s) || s[i+2] != 'Z'
}

func syscallKill(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}
