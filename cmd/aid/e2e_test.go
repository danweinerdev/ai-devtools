package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// aidBin is the real binary, built once for the end-to-end tests.
var aidBin string

func TestMain(m *testing.M) {
	// A developer's AID_HOLDER would change every default-holder outcome;
	// tests that want one set it on the child explicitly.
	os.Unsetenv("AID_HOLDER")
	dir, err := os.MkdirTemp("", "aid-bin-")
	if err != nil {
		panic(err)
	}
	aidBin = filepath.Join(dir, "aid")
	if runtime.GOOS == "windows" {
		aidBin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-tags", "aidtest", "-o", aidBin, ".").CombinedOutput(); err != nil {
		panic("building aid: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// repo is a temporary repository root the binary runs in.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	return &repo{t: t, dir: t.TempDir()}
}

// aid runs the binary in dir (relative to the repo) and returns stdout,
// stderr and the exit code.
func (r *repo) aidIn(sub string, args ...string) (string, string, int) {
	r.t.Helper()
	cmd := exec.Command(aidBin, args...)
	cmd.Dir = filepath.Join(r.dir, sub)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

func (r *repo) aid(args ...string) (string, string, int) {
	r.t.Helper()
	return r.aidIn("", args...)
}

// ok runs aid and fails the test unless it exits 0.
func (r *repo) ok(args ...string) string {
	r.t.Helper()
	out, errOut, code := r.aid(args...)
	if code != 0 {
		r.t.Fatalf("aid %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errOut)
	}
	return out
}

// refused runs aid, requires exit 1, and returns stderr.
func (r *repo) refused(args ...string) string {
	r.t.Helper()
	out, errOut, code := r.aid(args...)
	if code != 1 {
		r.t.Fatalf("aid %s: want exit 1, got %d\n%s%s", strings.Join(args, " "), code, out, errOut)
	}
	return errOut
}

func contains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
}

func TestSkeletonEndToEnd(t *testing.T) {
	r := newRepo(t)
	r.refused("ready") // no .aid yet
	contains(t, r.ok("init", "Feature"), "created .aid/Feature")
	contains(t, r.refused("init", "Feature"), "already exists")

	// Checks run from the repository root: the first proves it by reading a
	// file only the root has.
	os.WriteFile(filepath.Join(r.dir, "marker"), []byte("root"), 0o644)
	r.ok("add-task", "--id", "store", "--title", "the store", "--footprint", "internal/store/",
		"--result", "it stores", "--check", "test -f marker", "--check", "true")
	contains(t, r.refused("add-task", "--id", "store", "--title", "x", "--footprint", "a",
		"--result", "r", "--check", "true"), `"store" already exists`)
	contains(t, r.refused("add-task", "--id", "cli", "--title", "x", "--footprint", "a",
		"--result", "r", "--check", "true", "--needs", "nope"), "nope")
	r.ok("add-task", "--id", "cli", "--title", "the cli", "--needs", "store", "--footprint", "cmd/aid",
		"--result", "it runs", "--check", "true")

	// Only store is ready; cli waits on it.
	ready := r.ok("ready")
	contains(t, ready, "store")
	if strings.Contains(ready, "cli") {
		t.Fatalf("cli is ready before its need is done:\n%s", ready)
	}
	contains(t, r.refused("claim-task", "cli", "--holder", "mechanic"), "needs store (todo)")

	contains(t, r.refused("finish"), "store (todo)")
	r.ok("claim-task", "store", "--holder", "oracle")
	contains(t, r.refused("claim-task", "store", "--holder", "mechanic"), `held by "oracle"`)
	contains(t, r.refused("finish"), "claimed by oracle")
	contains(t, r.refused("complete-task", "store", "--holder", "mechanic"), `held by "oracle"`)
	contains(t, r.ok("complete-task", "store", "--holder", "oracle"), "done store")

	r.ok("claim-task", "cli", "--holder", "mechanic")
	r.ok("complete-task", "cli", "--holder", "mechanic")
	contains(t, r.ok("ready"), "nothing ready")
	contains(t, r.ok("finish"), "finished: 2 done, 0 dropped")
}

func TestSkeletonFailingCheckKeepsTaskDoing(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "r",
		"--check", "true", "--check", "echo broken && exit 3")
	r.ok("claim-task", "a", "--holder", "h")
	errOut := r.refused("complete-task", "a", "--holder", "h")
	contains(t, errOut, "stays in-progress")
	contains(t, errOut, "FAIL  echo broken && exit 3")
	contains(t, errOut, "broken")

	// --json carries the per-check results as structured detail.
	out, _, code := r.aid("complete-task", "a", "--holder", "h", "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var res struct {
		Error  string `json:"error"`
		Detail struct {
			Done   bool `json:"done"`
			Checks []struct {
				Passed   bool `json:"passed"`
				ExitCode int  `json:"exitCode"`
			} `json:"checks"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if res.Detail.Done || len(res.Detail.Checks) != 2 || !res.Detail.Checks[0].Passed || res.Detail.Checks[1].ExitCode != 3 {
		t.Fatalf("detail: %+v", res.Detail)
	}
	contains(t, r.refused("finish"), "a (in-progress, claimed by h)")
}

func TestSkeletonGeneratedIDsAndJSON(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	var added struct {
		ID        string `json:"id"`
		Effective string `json:"effective"`
		State     struct {
			Status string `json:"status"`
		} `json:"state"`
	}
	out := r.ok("add-task", "--json", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true")
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatal(err)
	}
	if len(added.ID) != 36 || added.ID[14] != '7' || added.State.Status != "todo" || added.Effective != "todo" {
		t.Fatalf("want a UUIDv7 todo task, got %+v", added)
	}
	var ready []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(r.ok("ready", "--json")), &ready); err != nil || len(ready) != 1 || ready[0].ID != added.ID {
		t.Fatalf("ready --json: %v %+v", err, ready)
	}
}

func TestSkeletonWorkSelection(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "One")
	os.MkdirAll(filepath.Join(r.dir, "deep", "sub"), 0o755)
	// From a subdirectory, the .aid above is found.
	if _, errOut, code := r.aidIn("deep/sub", "ready"); code != 0 {
		t.Fatalf("from a subdirectory: %s", errOut)
	}
	r.ok("init", "Two")
	contains(t, r.refused("ready"), "several works in")
	contains(t, r.refused("ready"), "One, Two")
	r.ok("ready", "--work", "Two")
	contains(t, r.refused("ready", "--work", "Three"), `no work "Three"`)
}

func TestSkeletonInvalidFileIsRefusedLoudly(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	path := filepath.Join(r.dir, ".aid", "Feature", "tasks.json")
	os.WriteFile(path, []byte(`{"version": 1, "taks": []}`), 0o644)
	errOut := r.refused("ready")
	contains(t, errOut, "tasks.json is invalid")
	contains(t, errOut, "did you mean `tasks`?")
}
