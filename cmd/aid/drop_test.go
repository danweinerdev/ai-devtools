package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// addTask adds a task with a passing check and the given needs.
func (r *repo) addTask(id string, needs ...string) {
	r.t.Helper()
	args := []string{"add-task", "--id", id, "--title", "build " + id, "--footprint", "src/" + id + ".go",
		"--result", id + " works", "--check", "true"}
	if len(needs) > 0 {
		args = append(args, "--needs", strings.Join(needs, ","))
	}
	r.ok(args...)
}

// finishTask claims and completes id.
func (r *repo) finishTask(id string) {
	r.t.Helper()
	r.ok("claim-task", id, "--holder", "h")
	r.ok("complete-task", id, "--holder", "h")
}

func TestDropRefusalAndBottomUp(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.addTask("a")
	r.addTask("b", "a")
	r.addTask("c", "b")
	r.addTask("d", "c")

	errOut := r.refused("drop-task", "a", "--reason", "gone")
	contains(t, errOut, `can't drop "a": 1 task(s) with work ahead need it directly`)
	contains(t, errOut, "b (todo)  [blocks]")
	contains(t, errOut, "2 more task(s)")
	contains(t, errOut, "--cascade")
	if strings.Contains(errOut, "d (") || strings.Contains(errOut, "→") {
		t.Fatalf("the refusal shows more than the first generation:\n%s", errOut)
	}

	// --json carries the direct dependents and the size of the subtree.
	out, _, code := r.aid("drop-task", "a", "--reason", "gone", "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var res struct {
		Detail struct {
			ID         string `json:"id"`
			Dependents []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Blocks bool   `json:"blocks"`
			} `json:"dependents"`
			Transitive int `json:"transitive"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if d := res.Detail; d.ID != "a" || len(d.Dependents) != 1 || d.Dependents[0].ID != "b" ||
		!d.Dependents[0].Blocks || d.Transitive != 2 {
		t.Fatalf("detail %+v", d)
	}

	contains(t, r.refused("drop-task", "a"), "reason") // --reason is required
	for _, id := range []string{"d", "c", "b", "a"} {
		contains(t, r.ok("drop-task", id, "--reason", "gone"), "dropped "+id+": gone")
	}
	contains(t, r.ok("ready"), "nothing ready")
	contains(t, r.ok("finish"), "finished: 0 done, 4 dropped")
}

func TestDropDoneDependentKeptAndReplacedBy(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.addTask("a")
	r.addTask("new")
	r.addTask("built", "a")
	r.addTask("next", "a")
	r.finishTask("a")
	r.finishTask("built")

	contains(t, r.refused("drop-task", "a", "--reason", "replaced"), "next (todo)  [blocks]")
	contains(t, r.refused("drop-task", "a", "--reason", "replaced", "--replaced-by", "nope"), `"nope" doesn't exist`)

	out := r.ok("drop-task", "a", "--reason", "replaced", "--replaced-by", "new")
	contains(t, out, "rewired onto the replacements: next")
	contains(t, out, "done, keeping their need as history: built")

	// Move next again, now onto a done task; built still needs a as history.
	r.ok("drop-task", "new", "--reason", "x", "--replaced-by", "built")
	data := r.ok("ready", "--json")
	var ready []struct {
		ID    string   `json:"id"`
		Needs []string `json:"needs"`
	}
	if err := json.Unmarshal([]byte(data), &ready); err != nil || len(ready) != 1 || ready[0].ID != "next" ||
		strings.Join(ready[0].Needs, ",") != "built" {
		t.Fatalf("ready: %v %s", err, data)
	}
}

func TestDropLeavesBatchAndFinish(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.addTask("a")
	r.ok("add-task", "--id", "broken", "--title", "t", "--footprint", "src/b.go", "--result", "r", "--check", "exit 1")
	r.finishTask("a")
	r.refused("check") // broken fails the batch
	contains(t, r.refused("finish"), "broken (todo)")

	out := r.ok("drop-task", "broken", "--reason", "not needed", "--json")
	var res struct {
		Task struct {
			ID    string `json:"id"`
			State struct {
				Status string `json:"status"`
				Reason string `json:"reason"`
			} `json:"state"`
		} `json:"task"`
		Rewired []string `json:"rewired"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Task.State.Status != "dropped" || res.Rewired == nil {
		t.Fatalf("%v %s", err, out)
	}
	contains(t, r.ok("check"), "1 passed, 0 failed")
	contains(t, r.ok("finish"), "finished: 1 done, 1 dropped")
	contains(t, r.refused("drop-task", "broken", "--reason", "again"), "already dropped")
}

func TestDropClaims(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.addTask("a")
	r.ok("claim-task", "a", "--holder", "oracle")
	contains(t, r.refused("drop-task", "a", "--reason", "x"), `claimed by "oracle"`)
	r.ok("claim-task", "a", "--holder", "oracle", "--lease", "1ns") // renew with a lease that lapses at once
	contains(t, r.ok("drop-task", "a", "--reason", "x"), "dropped a: x")
}

func TestDropCascadeEndToEnd(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.addTask("a")
	r.addTask("keep")
	r.addTask("b", "a")
	r.addTask("m", "a")
	r.addTask("c", "b")
	r.addTask("n", "m")
	r.finishTask("a")
	r.finishTask("m") // done: keeps its need on a, and n below it stays

	contains(t, r.refused("drop-task", "a", "--reason", "x", "--cascade", "--replaced-by", "keep"), "don't mix")

	// A live claim in the subtree refuses the whole cascade, changing nothing.
	r.ok("claim-task", "b", "--holder", "mechanic")
	before := r.ok("show", "--json")
	contains(t, r.refused("drop-task", "a", "--reason", "x", "--cascade"), `b (claimed by "mechanic"`)
	if after := r.ok("show", "--json"); after != before {
		t.Fatalf("a refused cascade changed the graph:\n%s\n%s", before, after)
	}
	r.ok("release-task", "b", "--holder", "mechanic")

	out := r.ok("drop-task", "a", "--reason", "scrapped", "--cascade")
	contains(t, out, "dropped a: scrapped")
	contains(t, out, "cascaded to: b, c")
	contains(t, out, "done, keeping their need as history: m")

	var res struct {
		Dropped       []string `json:"dropped"`
		KeptAsHistory []string `json:"keptAsHistory"`
	}
	r.ok("add-task", "--id", "z", "--title", "t", "--footprint", "src/z.go", "--result", "r", "--check", "true", "--needs", "keep")
	data := r.ok("drop-task", "keep", "--reason", "gone", "--cascade", "--json")
	if err := json.Unmarshal([]byte(data), &res); err != nil || strings.Join(res.Dropped, ",") != "keep,z" ||
		res.KeptAsHistory == nil {
		t.Fatalf("%v %s", err, data)
	}

	// n still needs the done m; it's the only work left, and the batch skips the dropped.
	contains(t, r.ok("ready"), "n\t")
	contains(t, r.ok("check"), "2 passed, 0 failed")
	r.finishTask("n")
	contains(t, r.ok("finish"), "finished: 2 done, 5 dropped")
}
