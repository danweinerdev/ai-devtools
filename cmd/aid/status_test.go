package main

import (
	"encoding/json"
	"testing"
)

func TestStatusReleaseBlockUnblock(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true")
	r.ok("claim-task", "a", "--holder", "oracle")

	contains(t, r.refused("release-task", "a", "--holder", "mechanic"), `held by "oracle"`)
	contains(t, r.ok("release-task", "a", "--holder", "oracle"), "released a (todo)")
	contains(t, r.refused("release-task", "a"), "is todo; only in-progress or blocked")

	r.ok("claim-task", "a", "--holder", "oracle")
	contains(t, r.refused("block-task", "a", "--reason", "vendor", "--holder", "mechanic"), "only its holder can block it")
	contains(t, r.refused("block-task", "a", "--reason", " ", "--holder", "oracle"), "needs a reason")
	contains(t, r.ok("block-task", "a", "--reason", "waiting on vendor", "--holder", "oracle"), "blocked a (blocked)")
	contains(t, r.refused("claim-task", "a", "--holder", "oracle"), "is blocked")
	contains(t, r.refused("finish"), "a (blocked)")

	var unblocked struct {
		State struct {
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"state"`
	}
	if err := json.Unmarshal([]byte(r.ok("release-task", "a", "--json")), &unblocked); err != nil {
		t.Fatal(err)
	}
	if unblocked.State.Status != "todo" || unblocked.State.Reason != "" {
		t.Fatalf("unblock: %+v", unblocked.State)
	}
	r.ok("claim-task", "a", "--holder", "mechanic")
	r.ok("complete-task", "a", "--holder", "mechanic")
	contains(t, r.ok("finish"), "finished: 1 done")
}

func TestStatusShortLeaseLapsesForAnotherHolder(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "r", "--check", "true")
	r.ok("claim-task", "a", "--holder", "oracle", "--lease", "1ns")
	// The 1ns lease has lapsed by the next command: a lapsed lease releases itself.
	contains(t, r.ok("ready"), "a\t")
	contains(t, r.ok("claim-task", "a", "--holder", "mechanic"), "claimed a for mechanic")
	contains(t, r.refused("complete-task", "a", "--holder", "oracle"), `held by "mechanic", not "oracle"`)
	contains(t, r.refused("claim-task", "a", "--holder", "x", "--lease", "0s"), "lease must be positive")
}
