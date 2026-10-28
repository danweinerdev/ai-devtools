package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"
)

// batchRepo has three tasks: store (passing, footprint a directory with a
// trailing slash), cli (failing, footprint a bare directory), and docs
// (passing, footprint one file).
func batchRepo(t *testing.T) *repo {
	t.Helper()
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "store", "--title", "s", "--footprint", "internal/store/", "--result", "r", "--check", "true")
	r.ok("add-task", "--id", "cli", "--title", "c", "--footprint", "cmd/aid", "--result", "r",
		"--check", "true", "--check", "echo cli-broken && exit 4")
	r.ok("add-task", "--id", "docs", "--title", "d", "--footprint", "README.md", "--result", "r", "--check", "true")
	return r
}

func TestCheckFullBatch(t *testing.T) {
	r := batchRepo(t)
	errOut := r.refused("check")
	for _, want := range []string{"pass  store (todo)", "FAIL  cli (todo)", "FAIL  echo cli-broken && exit 4", "cli-broken",
		"pass  docs (todo)", "2 passed, 1 failed"} {
		contains(t, errOut, want)
	}
}

func TestCheckPathsSelect(t *testing.T) {
	r := batchRepo(t)
	for _, tc := range []struct {
		paths    string
		want     []string
		fails    bool
		excluded []string
	}{
		{"internal/store/lock.go", []string{"store"}, false, []string{"cli", "docs"}},
		{"internal/store", []string{"store"}, false, []string{"cli"}},
		{"cmd/aid/main.go", []string{"cli"}, true, []string{"store"}},
		{"README.md,internal/store/x.go", []string{"store", "docs"}, false, []string{"cli"}},
	} {
		out, errOut, code := r.aid("check", "--paths", tc.paths)
		got := out + errOut
		if (code == 1) != tc.fails {
			t.Errorf("--paths %s: exit %d\n%s", tc.paths, code, got)
		}
		for _, id := range tc.want {
			if !strings.Contains(got, " "+id+" (") {
				t.Errorf("--paths %s: want %s in\n%s", tc.paths, id, got)
			}
		}
		for _, id := range tc.excluded {
			if strings.Contains(got, " "+id+" (") {
				t.Errorf("--paths %s: %s must not run\n%s", tc.paths, id, got)
			}
		}
	}
	// Segment boundary: cmd/ai isn't under cmd/aid.
	contains(t, r.ok("check", "--paths", "cmd/ai"), "no tasks selected")
	contains(t, r.ok("check", "--paths", "docs/other.md"), "no tasks selected")
}

func TestCheckPathsRelativeToCwdAndOutsideRoot(t *testing.T) {
	r := batchRepo(t)
	os.MkdirAll(filepath.Join(r.dir, "internal", "store"), 0o755)
	out, errOut, code := r.aidIn("internal", "check", "--paths", "store/lock.go")
	if code != 0 || !strings.Contains(out, "pass  store") {
		t.Fatalf("relative to cwd: exit %d\n%s%s", code, out, errOut)
	}
	out, errOut, code = r.aidIn("", "check", "--paths", filepath.Join(r.dir, "README.md"))
	if code != 0 || !strings.Contains(out, "pass  docs") {
		t.Fatalf("absolute path: exit %d\n%s%s", code, out, errOut)
	}
	contains(t, r.refused("check", "--paths", "../elsewhere.go"), "outside the repository root")
}

func TestCheckExcludesDropped(t *testing.T) {
	r := batchRepo(t)
	// drop-task doesn't exist yet: mark cli dropped through the store.
	w, err := store.Open(filepath.Join(r.dir, AidDir), "Feature")
	if err != nil {
		t.Fatal(err)
	}
	err = w.Update(func(tx *store.Tx) error {
		g, err := loadGraph(tx)
		if err != nil {
			return err
		}
		i, err := index(g, "cli")
		if err != nil {
			return err
		}
		g.Tasks[i].State = graph.State{Status: graph.Dropped, Reason: "replaced"}
		return saveGraph(tx, g)
	})
	if err != nil {
		t.Fatal(err)
	}
	out := r.ok("check")
	contains(t, out, "2 passed, 0 failed")
	if strings.Contains(out, "cli") {
		t.Fatalf("dropped task ran:\n%s", out)
	}
	contains(t, r.ok("check", "--paths", "cmd/aid/main.go"), "no tasks selected")
}

func TestCheckJSON(t *testing.T) {
	r := batchRepo(t)
	out := r.ok("check", "--json", "--paths", "internal/store/")
	var ok batchResult
	if err := json.Unmarshal([]byte(out), &ok); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if ok.Passed != 1 || ok.Failed != 0 || len(ok.Tasks) != 1 || ok.Tasks[0].ID != "store" || !ok.Tasks[0].Passed ||
		ok.Tasks[0].Status != "todo" || len(ok.Tasks[0].Checks) != 1 {
		t.Fatalf("success: %+v", ok)
	}

	out, _, code := r.aid("check", "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var failed struct {
		Error  string      `json:"error"`
		Detail batchResult `json:"detail"`
	}
	if err := json.Unmarshal([]byte(out), &failed); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	d := failed.Detail
	if d.Passed != 2 || d.Failed != 1 || len(d.Tasks) != 3 || d.Tasks[1].Passed || d.Tasks[1].Checks[1].ExitCode != 4 {
		t.Fatalf("failure detail: %+v", d)
	}

	var empty batchResult
	if err := json.Unmarshal([]byte(r.ok("check", "--json", "--paths", "nowhere")), &empty); err != nil ||
		empty.Tasks == nil || len(empty.Tasks) != 0 {
		t.Fatalf("empty selection: %v %+v", err, empty)
	}
}

func TestCheckUncoveredPathsPassUnlessStrict(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "internal/store", "--result", "r", "--check", "true")

	// An unowned path alone: listed, a pass by default, a failure with --strict.
	out := r.ok("check", "--paths", "README.md")
	contains(t, out, "no task covers: README.md")
	contains(t, out, "nothing to check")
	contains(t, r.refused("check", "--paths", "README.md", "--strict"), "--strict")

	// Partly covered: the covered task runs; --strict still fails on the rest.
	out = r.ok("check", "--paths", "internal/store/txn.go,README.md")
	contains(t, out, "pass  a")
	contains(t, out, "no task covers: README.md")
	contains(t, r.refused("check", "--paths", "internal/store/txn.go,README.md", "--strict"), "some paths have no task")

	// Fully covered passes even with --strict; --strict alone is an error.
	r.ok("check", "--paths", "internal/store/txn.go", "--strict")
	contains(t, r.refused("check", "--strict"), "--strict needs --paths")

	var res struct {
		Uncovered []string `json:"uncovered"`
	}
	if err := json.Unmarshal([]byte(r.ok("check", "--paths", "README.md", "--json")), &res); err != nil || len(res.Uncovered) != 1 {
		t.Fatalf("--json uncovered: %v %+v", err, res)
	}
}
