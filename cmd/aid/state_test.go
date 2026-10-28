package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateItemsLifecycle(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	if _, err := os.Stat(filepath.Join(r.dir, ".aid", "Feature", "state.json")); err != nil {
		t.Fatal("init didn't seed state.json:", err)
	}
	contains(t, r.ok("items"), "no items")

	contains(t, r.ok("add-item", "--kind", "note", "--text", "lock is per work"), "added i1")
	contains(t, r.ok("add-item", "--kind", "follow-up", "--text", "port guards"), "added i2")
	contains(t, r.ok("add-item", "--kind", "question", "--text", "lease length?"), "added i3")
	contains(t, r.refused("add-item", "--kind", "todo", "--text", "x"), `--kind "todo" must be one of`)

	// Filters.
	got := r.ok("items", "--kind", "follow-up")
	contains(t, got, "i2\tfollow-up\topen\tport guards")
	if strings.Contains(got, "i1") || strings.Contains(got, "i3") {
		t.Fatalf("kind filter leaked:\n%s", got)
	}
	contains(t, r.refused("items", "--status", "closed"), `--status "closed" must be one of`)

	// update-item changes only the given flags.
	contains(t, r.refused("update-item", "i1"), "nothing to change")
	r.ok("update-item", "i1", "--kind", "finding")
	contains(t, r.ok("items", "--kind", "finding"), "i1\tfinding\topen\tlock is per work")
	contains(t, r.refused("update-item", "i9", "--text", "x"), `no item "i9"`)

	// Resolve, and resolved items can still be edited but not re-resolved.
	r.ok("resolve-item", "i1", "--as", "done", "--where", "internal/store/lock.go")
	contains(t, r.refused("resolve-item", "i1", "--as", "prd", "--where", "x"), "already resolved")
	r.ok("update-item", "i1", "--text", "lock is per work directory")
	contains(t, r.ok("items", "--status", "resolved"), "i1\tfinding\tresolved\tlock is per work directory\t→ done: internal/store/lock.go")
	open := r.ok("items", "--status", "open")
	if strings.Contains(open, "i1") || !strings.Contains(open, "i2") {
		t.Fatalf("status filter:\n%s", open)
	}
	contains(t, r.refused("resolve-item", "i2", "--as", "elsewhere", "--where", "x"), `--as "elsewhere" must be one of`)
}

func TestStateIDsAreNeverReused(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-item", "--kind", "note", "--text", "a")
	r.ok("add-item", "--kind", "note", "--text", "b")
	r.ok("resolve-item", "i2", "--as", "done", "--where", "here")
	contains(t, r.ok("add-item", "--kind", "note", "--text", "c"), "added i3")

	var items []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(r.ok("items", "--json")), &items); err != nil || len(items) != 3 || items[2].ID != "i3" {
		t.Fatalf("items --json: %v %+v", err, items)
	}
}

func TestStateResolveAsTaskNeedsAnExistingTask(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-item", "--kind", "follow-up", "--text", "split the store")
	contains(t, r.refused("resolve-item", "i1", "--as", "task", "--where", "store-split"), `no task "store-split"`)
	contains(t, r.ok("items", "--status", "open"), "i1") // nothing written
	r.ok("add-task", "--id", "store-split", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true")
	contains(t, r.ok("resolve-item", "i1", "--as", "task", "--where", "store-split"), "resolved i1 → task: store-split")
}

func TestStateFinishRefusesOpenItems(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true")
	r.ok("add-item", "--kind", "question", "--text", "lease length?")

	// Both the task and the item are reported.
	errOut := r.refused("finish")
	contains(t, errOut, "a (todo)")
	contains(t, errOut, "i1 (question) lease length?")

	r.ok("claim-task", "a", "--holder", "h")
	r.ok("complete-task", "a", "--holder", "h")
	errOut = r.refused("finish")
	contains(t, errOut, "1 item(s) open")
	if strings.Contains(errOut, "task(s) left") {
		t.Fatalf("no tasks are left:\n%s", errOut)
	}

	out, _, code := r.aid("finish", "--json")
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	var res struct {
		Detail struct {
			Tasks []any `json:"tasks"`
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(res.Detail.Tasks) != 0 || len(res.Detail.Items) != 1 || res.Detail.Items[0].ID != "i1" {
		t.Fatalf("detail: %+v", res.Detail)
	}

	r.ok("resolve-item", "i1", "--as", "guidance", "--where", "skills/aid-graph")
	contains(t, r.ok("finish"), "finished: 1 done, 0 dropped")
}

func TestStateInvalidFileIsRefusedLoudly(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	path := filepath.Join(r.dir, ".aid", "Feature", "state.json")
	os.WriteFile(path, []byte(`{"version": 1, "items": [{"id": "i1", "kind": "note", "status": "resolved", "text": "t"}]}`), 0o644)
	contains(t, r.refused("items"), "a resolved item needs a resolution")
	contains(t, r.refused("finish"), "state.json is invalid")
}
