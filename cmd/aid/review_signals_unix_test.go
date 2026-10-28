//go:build !windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// alive reports whether pid still exists.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// waitFor polls cond for up to d.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// TestSignalStopsChecks: SIGTERM or SIGINT to aid while a check runs must
// cancel the check, kill everything it started, and leave the task undone.
func TestSignalStopsChecks(t *testing.T) {
	for _, c := range []struct {
		name string
		sig  syscall.Signal
		args []string
	}{
		{"complete-task SIGTERM", syscall.SIGTERM, []string{"complete-task", "t", "--holder", "h"}},
		{"check SIGINT", syscall.SIGINT, []string{"check"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRepo(t)
			pidfile := filepath.Join(t.TempDir(), "pid")
			r.ok("init", "Feature")
			r.ok("add-task", "--id", "t", "--title", "t", "--footprint", "a.go", "--result", "r",
				"--check", "sleep 777 & echo $! > "+pidfile+"; wait")
			r.ok("claim-task", "t", "--holder", "h")

			cmd := exec.Command(aidBin, c.args...)
			cmd.Dir = r.dir
			var errOut bytes.Buffer
			cmd.Stderr = &errOut
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			var pid int
			if !waitFor(10*time.Second, func() bool {
				b, err := os.ReadFile(pidfile)
				if err != nil {
					return false
				}
				pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
				return err == nil
			}) {
				cmd.Process.Kill()
				t.Fatal("the check never started")
			}
			// Whatever happens below, never leave the check's child behind.
			t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
			start := time.Now()
			if err := cmd.Process.Signal(c.sig); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			if took := time.Since(start); took > 8*time.Second {
				t.Fatalf("aid took %s to stop", took)
			}
			ee, ok := err.(*exec.ExitError)
			if !ok || ee.ExitCode() != 1 {
				t.Fatalf("want a clean exit 1 (a refusal), got %v\n%s", err, errOut.String())
			}
			if !waitFor(5*time.Second, func() bool { return !alive(pid) }) {
				syscall.Kill(pid, syscall.SIGKILL)
				t.Fatalf("the check's child %d outlived aid", pid)
			}
			var shown []struct {
				State struct {
					Status string `json:"status"`
				} `json:"state"`
			}
			if err := json.Unmarshal([]byte(r.ok("show", "t", "--json")), &shown); err != nil || len(shown) != 1 {
				t.Fatalf("show: %v", err)
			}
			if shown[0].State.Status != "in-progress" {
				t.Fatalf("task is %s after an interrupted run, want in-progress", shown[0].State.Status)
			}
		})
	}
}
