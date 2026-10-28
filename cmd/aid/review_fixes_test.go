package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// inProcess runs one aid command in this process against dir.
func inProcess(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	a := &app{cwd: dir, now: time.Now, out: &out, err: &errOut}
	code := run(context.Background(), a, args)
	return out.String() + errOut.String(), code
}

// TestFinishRetryCountsOnce: when a file changes under the lock, Update
// re-runs finish's scan; the tallies must not accumulate across runs.
func TestFinishRetryCountsOnce(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "Feature"},
		{"add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true"},
		{"claim-task", "a", "--holder", "h"},
		{"complete-task", "a", "--holder", "h"},
	} {
		if out, code := inProcess(t, dir, args...); code != 0 {
			t.Fatalf("%v: %s", args, out)
		}
	}
	tasks := filepath.Join(dir, ".aid", "Feature", "tasks.json")
	passes := 0
	afterFinishScan = func() {
		passes++
		if passes == 1 { // an outside writer touches the file once: valid JSON, new digest
			b, _ := os.ReadFile(tasks)
			os.WriteFile(tasks, append(b, '\n'), 0o644)
		}
	}
	defer func() { afterFinishScan = func() {} }()
	out, code := inProcess(t, dir, "finish")
	if code != 0 || passes != 2 {
		t.Fatalf("exit %d after %d passes: %s", code, passes, out)
	}
	if !strings.Contains(out, "finished: 1 done, 0 dropped") {
		t.Fatalf("tallies accumulated across the retry: %s", out)
	}
}

// TestShippedBinaryIgnoresAIDNow: only the aidtest build reads AID_NOW.
func TestShippedBinaryIgnoresAIDNow(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "aid")
	if runtime.GOOS == "windows" {
		plain += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", plain, ".").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	expiryYear := func(bin string) int {
		dir := t.TempDir()
		runIn := func(args ...string) string {
			cmd := exec.Command(bin, args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "AID_NOW=2001-02-03T04:05:06Z")
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			return string(out)
		}
		runIn("init", "Feature")
		runIn("add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true")
		var claimed struct {
			State struct {
				Claim struct {
					Expires time.Time `json:"expires"`
				} `json:"claim"`
			} `json:"state"`
		}
		if err := json.Unmarshal([]byte(runIn("claim-task", "a", "--holder", "h", "--json")), &claimed); err != nil {
			t.Fatal(err)
		}
		return claimed.State.Claim.Expires.Year()
	}
	if y := expiryYear(aidBin); y != 2001 {
		t.Fatalf("control: the aidtest build should pin the clock, expiry year %d", y)
	}
	if y := expiryYear(plain); y == 2001 {
		t.Fatal("the shipped binary honoured AID_NOW")
	}
}

// TestWorksSkipUninitialisedDirectories: a breakdown written before init
// doesn't make a second work.
func TestWorksSkipUninitialisedDirectories(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "One")
	draft := filepath.Join(r.dir, ".aid", "Draft")
	os.MkdirAll(draft, 0o755)
	os.WriteFile(filepath.Join(draft, "breakdown.md"), []byte("# plan\n"), 0o644)
	r.ok("ready") // still exactly one work
	r.ok("init", "Draft")
	contains(t, r.refused("ready"), "several works")
}

// TestGetwdFailureIsClear: a deleted working directory is a clear error.
func TestGetwdFailureIsClear(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("relies on Linux getcwd failing for a removed directory")
	}
	base := t.TempDir()
	cmd := exec.Command("sh", "-c", `mkdir gone && cd gone && rmdir ../gone && exec "$0" ready`, aidBin)
	cmd.Dir = base
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got %v\n%s", err, out)
	}
	contains(t, string(out), "can't determine the current directory")
}
