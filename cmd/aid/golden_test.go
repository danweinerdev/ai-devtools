package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

// goldenNow pins the clock (AID_NOW, test-only) so output is reproducible.
const goldenNow = "2026-01-02T03:04:05Z"

// golden runs aid with the pinned clock and returns stdout, stderr and the
// exit code.
func (r *repo) golden(args ...string) (string, string, int) {
	r.t.Helper()
	cmd := exec.Command(aidBin, args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "AID_NOW="+goldenNow)
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

var durations = []*regexp.Regexp{
	regexp.MustCompile(`"seconds": [0-9.]+`),
	regexp.MustCompile(`\([0-9.]+s\)`),
}

// normalise removes what legitimately varies between runs: the temp
// repository path and check durations.
func (r *repo) normalise(s string) string {
	s = strings.ReplaceAll(s, r.dir, "$REPO")
	s = durations[0].ReplaceAllString(s, `"seconds": 0`)
	return durations[1].ReplaceAllString(s, "(Ns)")
}

// expect runs aid, requires the exit code, and compares the output to
// testdata/golden/<name>: stdout on success, stdout and stderr on refusal.
func (r *repo) expect(name string, code int, args ...string) {
	r.t.Helper()
	out, errOut, got := r.golden(args...)
	if got != code {
		r.t.Fatalf("aid %s: exit %d, want %d\n%s%s", strings.Join(args, " "), got, code, out, errOut)
	}
	content := out
	if code != 0 {
		content = "-- stdout --\n" + out + "-- stderr --\n" + errOut
	}
	content = r.normalise(content)
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			r.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatalf("%v (run `go test ./cmd/aid -run TestGolden -update` to create it)", err)
	}
	if string(want) != content {
		r.t.Errorf("%s differs from the golden file (rerun with -update if the change is intended)\n--- want\n%s\n--- got\n%s", name, want, content)
	}
}

func (r *repo) step(args ...string) {
	r.t.Helper()
	if _, errOut, code := r.golden(args...); code != 0 {
		r.t.Fatalf("aid %s: exit %d\n%s", strings.Join(args, " "), code, errOut)
	}
}

// TestGoldenEveryCommand drives one piece of work through every command and
// pins each one's output.
func TestGoldenEveryCommand(t *testing.T) {
	r := newRepo(t)
	r.expect("init.json", 0, "init", "Feature", "--json")
	r.expect("add-task.json", 0, "add-task", "--json", "--id", "store", "--title", "The store (Make)",
		"--footprint", "internal/store/", "--result", "it stores", "--check", "true")
	r.step("add-task", "--id", "cli", "--title", "The CLI", "--needs", "store", "--footprint", "cmd/aid",
		"--result", "it runs", "--check", "true")
	r.step("add-task", "--id", "docs", "--title", "Docs", "--needs", "store", "--footprint", "README.md",
		"--result", "documented", "--check", "true")
	r.expect("update-task.json", 0, "update-task", "docs", "--json", "--title", "The docs")
	r.step("add-task", "--id", "docs-guide", "--title", "Guide", "--footprint", "skills/", "--result", "r", "--check", "true")
	r.step("add-task", "--id", "docs-readme", "--title", "README", "--footprint", "README.md", "--result", "r", "--check", "true")
	r.expect("split-task.json", 0, "split-task", "docs", "--into", "docs-guide,docs-readme", "--json")
	r.expect("ready.json", 0, "ready", "--json")
	r.expect("claim-task.json", 0, "claim-task", "store", "--holder", "oracle", "--json")

	r.expect("show-graph.txt", 0, "show")
	r.expect("show-graph.json", 0, "show", "--json")
	r.expect("show-tasks.txt", 0, "show", "store", "docs")
	r.expect("show-tasks.json", 0, "show", "store", "docs", "--json")

	r.expect("complete-task.json", 0, "complete-task", "store", "--holder", "oracle", "--json")
	r.step("claim-task", "cli", "--holder", "mechanic")
	r.expect("release-task.json", 0, "release-task", "cli", "--holder", "mechanic", "--json")
	r.expect("block-task.json", 0, "block-task", "cli", "--reason", "waiting on a decision", "--json")
	r.expect("check.json", 0, "check", "--paths", "internal/store/decode.go", "--json")

	r.expect("add-item.json", 0, "add-item", "--kind", "note", "--text", "a surprising behaviour", "--json")
	r.expect("update-item.json", 0, "update-item", "i1", "--text", "a surprising lock behaviour", "--json")
	r.expect("resolve-item.json", 0, "resolve-item", "i1", "--as", "task", "--where", "store", "--json")
	r.step("add-item", "--kind", "follow-up", "--text", "port the guards")
	r.expect("items.json", 0, "items", "--json")
	r.expect("finish.json", 1, "finish", "--json")

	r.expect("validate-valid.txt", 0, "validate")
	r.expect("validate-valid.json", 0, "validate", "--json")
}

// writeWork overwrites one of the work's files.
func (r *repo) writeWork(file, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, ".aid", "Feature", file), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func TestValidateReportsEveryProblemInBothFiles(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	r.writeWork("tasks.json", `{"version": 1, "tasks": [
  {"id": "a", "title": "", "needs": ["ghost"], "footprint": ["x"], "done": {"result": "r", "checks": ["true"]}, "state": {"status": "todo"}},
  {"id": "Bad ID", "title": "t", "needs": [], "footprint": ["x"], "done": {"result": "r", "checks": []}, "state": {"status": "todo"}}
]}`)
	r.writeWork("state.json", `{"version": 1, "items": [{"id": "i1", "kind": "idea", "status": "open", "text": "x"}], "extra": 1}`)
	r.expect("validate-invalid.txt", 1, "validate")
	r.expect("validate-invalid.json", 1, "validate", "--json")
}

func TestValidateCrossFileTaskResolution(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	r.step("add-task", "--id", "a", "--title", "t", "--footprint", "x", "--result", "r", "--check", "true")
	r.step("add-item", "--kind", "follow-up", "--text", "x")
	r.step("resolve-item", "i1", "--as", "task", "--where", "a")
	r.ok("validate")
	// Rename the task behind aid's back: the item now points at nothing.
	path := filepath.Join(r.dir, ".aid", "Feature", "tasks.json")
	raw, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(raw), `"id": "a"`, `"id": "b"`, 1)), 0o644)
	contains(t, r.refused("validate"), `item i1 is resolved as task "a", which doesn't exist in tasks.json`)
}

func TestShowUnknownIDAndEmptyGraph(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	contains(t, r.ok("show"), "no tasks\n0 tasks; 0 open items, 0 resolved")
	contains(t, r.refused("show", "nope"), `no task "nope"`)
	contains(t, r.ok("show", "--json"), `"tasks": []`)
}

func TestShowLapsedLease(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	r.step("add-task", "--id", "a", "--title", "t", "--footprint", "x", "--result", "r", "--check", "true")
	r.step("claim-task", "a", "--holder", "h", "--lease", "1ns")
	out := r.ok("show", "a")
	contains(t, out, "status:     todo (lease lapsed; stored as in-progress)")
	contains(t, out, "claim:      h until")
}

// TestGoldenDropTask pins drop-task's refusal (a direct dependent with work
// ahead, a transitive one, a done one kept as history) and its success with
// --replaced-by, in text and --json.
func TestGoldenDropTask(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	add := func(id string, needs ...string) {
		args := []string{"add-task", "--id", id, "--title", "Task " + id, "--footprint", id + ".go",
			"--result", "r", "--check", "true"}
		if len(needs) > 0 {
			args = append(args, "--needs", strings.Join(needs, ","))
		}
		r.step(args...)
	}
	add("a")
	add("b", "a")
	add("c", "b")
	add("d", "a")
	add("r")
	for _, id := range []string{"a", "d"} {
		r.step("claim-task", id, "--holder", "h")
		r.step("complete-task", id, "--holder", "h")
	}
	r.expect("drop-task-refused.txt", 1, "drop-task", "a", "--reason", "superseded")
	r.expect("drop-task-refused.json", 1, "drop-task", "a", "--reason", "superseded", "--json")
	r.expect("drop-task.txt", 0, "drop-task", "a", "--reason", "superseded by r", "--replaced-by", "r")
	r.expect("show-after-drop.txt", 0, "show")
}

// TestGoldenDropTaskJSON pins the --json success shape separately, since a
// task can only be dropped once.
func TestGoldenDropTaskJSON(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	r.step("add-task", "--id", "a", "--title", "Task a", "--footprint", "a.go", "--result", "r", "--check", "true")
	r.step("add-task", "--id", "b", "--title", "Task b", "--needs", "a", "--footprint", "b.go", "--result", "r", "--check", "true")
	r.step("add-task", "--id", "r", "--title", "Task r", "--footprint", "r.go", "--result", "r", "--check", "true")
	r.expect("drop-task.json", 0, "drop-task", "a", "--reason", "superseded by r", "--replaced-by", "r", "--json")
}

// TestGoldenDropTaskCascade pins --cascade: the whole subtree with work ahead
// goes, a done dependent keeps its need, in text and --json.
func TestGoldenDropTaskCascade(t *testing.T) {
	for _, c := range []struct {
		file string
		json bool
	}{{"drop-task-cascade.txt", false}, {"drop-task-cascade.json", true}} {
		r := newRepo(t)
		r.step("init", "Feature")
		for _, a := range [][]string{{"a"}, {"b", "a"}, {"c", "b"}, {"d", "a"}} {
			args := []string{"add-task", "--id", a[0], "--title", "Task " + a[0], "--footprint", a[0] + ".go",
				"--result", "r", "--check", "true"}
			if len(a) > 1 {
				args = append(args, "--needs", a[1])
			}
			r.step(args...)
		}
		r.step("claim-task", "a", "--holder", "h")
		r.step("complete-task", "a", "--holder", "h")
		r.step("claim-task", "d", "--holder", "h")
		r.step("complete-task", "d", "--holder", "h")
		args := []string{"drop-task", "a", "--reason", "scrapped", "--cascade"}
		if c.json {
			args = append(args, "--json")
		}
		r.expect(c.file, 0, args...)
	}
}
