package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// addTask adds a todo task with defaults the edit tests don't care about.
func addTask(r *repo, id string, needs ...string) {
	r.t.Helper()
	args := []string{"add-task", "--id", id, "--title", "build " + id, "--footprint", id + ".go",
		"--result", id + " works", "--check", "true"}
	if len(needs) > 0 {
		args = append(args, "--needs", strings.Join(needs, ","))
	}
	r.ok(args...)
}

type shownTask struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Needs     []string `json:"needs"`
	Footprint []string `json:"footprint"`
	Done      struct {
		Result string   `json:"result"`
		Checks []string `json:"checks"`
	} `json:"done"`
	State struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"state"`
}

func TestEditUpdateTask(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	addTask(r, "a")
	addTask(r, "b", "a")

	out := r.ok("update-task", "b", "--title", "renamed", "--check", "true", "--check", "go test ./b, -run X")
	contains(t, out, "updated b: title, check")
	var got shownTask
	if err := json.Unmarshal([]byte(r.ok("update-task", "b", "--result", "it works", "--reason", "clearer", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "renamed" || got.Done.Result != "it works" || len(got.Done.Checks) != 2 ||
		got.Done.Checks[1] != "go test ./b, -run X" || len(got.Needs) != 1 || got.Footprint[0] != "b.go" ||
		got.State.Status != "todo" {
		t.Fatalf("update applied wrongly: %+v", got)
	}

	// --needs= clears the list; a cycle is refused and nothing is written.
	r.ok("update-task", "b", "--needs=")
	contains(t, r.ok("ready"), "b")
	r.ok("update-task", "b", "--needs", "a")
	contains(t, r.refused("update-task", "a", "--needs", "b"), "cycle: ")
	contains(t, r.ok("ready"), "a")

	contains(t, r.refused("update-task", "a"), "nothing to update")
	contains(t, r.refused("update-task", "zz", "--title", "x"), `no task "zz"`)

	// A done task can't gain a need that isn't done.
	r.ok("claim-task", "a", "--holder", "h")
	r.ok("complete-task", "a", "--holder", "h")
	addTask(r, "c")
	contains(t, r.refused("update-task", "a", "--needs", "c"), "is done but needs `c`")
}

// A live claim freezes the task: neither another holder nor the holder itself
// can swap the checks for something weaker and then complete it.
func TestEditUpdateTaskRefusedUnderALiveClaim(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "x", "--result", "r", "--check", "false")
	r.ok("claim-task", "a", "--holder", "oracle")
	contains(t, r.refused("update-task", "a", "--check", "true"), `task "a" is held by "oracle"`)
	contains(t, r.refused("update-task", "a", "--title", "x"), "release it first, then edit it")
	t.Setenv("AID_HOLDER", "oracle")
	contains(t, r.refused("update-task", "a", "--check", "true"), "release it first, then edit it")
	contains(t, r.refused("complete-task", "a"), "a check failed")

	r.ok("release-task", "a")
	contains(t, r.refused("update-task", "a", "--check", "true"), "removes or changes the check(s) `false`")
	out := r.ok("update-task", "a", "--check", "true", "--reason", "the probe")
	contains(t, out, "weakened its definition of done; recorded in its history: the probe")
	contains(t, r.ok("show", "a"), "weakened by oracle: the probe")
}

func TestEditSplitTask(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	addTask(r, "base")
	addTask(r, "big", "base")
	addTask(r, "user", "big")
	addTask(r, "p1")
	addTask(r, "p2")

	out := r.ok("split-task", "big", "--into", "p1,p2")
	contains(t, out, "split big into p1, p2 (dropped)")
	contains(t, out, "now needing the parts: user")

	var tasks []shownTask
	r.ok("add-task", "--id", "late", "--title", "t", "--footprint", "x", "--result", "r", "--check", "true")
	if err := json.Unmarshal([]byte(r.ok("update-task", "late", "--title", "t2", "--json")), new(shownTask)); err != nil {
		t.Fatal(err)
	}
	addTask(r, "q")
	addTask(r, "big2", "late")
	if err := json.Unmarshal([]byte(r.ok("split-task", "big2", "--into", "q", "--json")), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 || tasks[0].ID != "big2" || tasks[0].State.Status != "dropped" ||
		tasks[0].State.Reason != "split into q" || tasks[1].ID != "q" || strings.Join(tasks[1].Needs, ",") != "late" {
		t.Fatalf("split --json: %+v", tasks)
	}

	// The rewired graph drives the frontier: user waits on both parts.
	r.ok("claim-task", "base", "--holder", "h")
	r.ok("complete-task", "base", "--holder", "h")
	ready := r.ok("ready")
	for _, want := range []string{"p1", "p2"} {
		contains(t, ready, want)
	}
	if strings.Contains(ready, "user") || strings.Contains(ready, "big\t") {
		t.Fatalf("user or the dropped task is ready:\n%s", ready)
	}

	contains(t, r.refused("split-task", "big", "--into", "p1"), "is dropped")
	contains(t, r.refused("split-task", "base", "--into", "p1"), "is done")
	addTask(r, "held")
	r.ok("claim-task", "held", "--holder", "h")
	contains(t, r.refused("split-task", "held", "--into", "p1"), `claimed by "h"`)
	contains(t, r.refused("split-task", "user", "--into", "nope"), `part "nope" doesn't exist`)
	contains(t, r.refused("update-task", "big", "--title", "x"), "is dropped")
}

func TestEditSplitCycleRefused(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	addTask(r, "big")
	addTask(r, "user", "big")
	addTask(r, "p", "user")
	contains(t, r.refused("split-task", "big", "--into", "p"), "cycle: ")
	// Nothing was written: big is still todo and ready.
	contains(t, r.ok("ready"), "big")
}
