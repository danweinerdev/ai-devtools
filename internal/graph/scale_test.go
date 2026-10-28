package graph

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// G6: big graphs stay fast and never recurse deeply. Bounds are generous so
// they hold under -race; they catch quadratic regressions, not microseconds.

func within(t *testing.T, what string, limit time.Duration, f func()) {
	t.Helper()
	start := time.Now()
	f()
	if d := time.Since(start); d > limit {
		t.Fatalf("%s took %s (limit %s)", what, d, limit)
	}
}

func chain(n int, status string) *Graph {
	tasks := make([]Task, n)
	for i := range n {
		var needs []string
		if i > 0 {
			needs = []string{fmt.Sprintf("t%d", i-1)}
		}
		tasks[i] = task(fmt.Sprintf("t%d", i), status, needs...)
	}
	return graphOf(tasks...)
}

func TestScaleLongChain(t *testing.T) {
	g := chain(100_000, Todo)
	within(t, "validate a 100k chain", 2*time.Second, func() { mustValid(t, g) })
	within(t, "ready on a 100k chain", 2*time.Second, func() {
		if r := g.Ready(now); len(r) != 1 || r[0].ID != "t0" {
			t.Fatalf("ready %d", len(r))
		}
	})
	// A 100k loop: the cycle check is iterative, so depth can't overflow.
	g.Tasks[0].Needs = []string{"t99999"}
	within(t, "find a 100k-long cycle", 2*time.Second, func() {
		if c := cyclesOf(g); len(c) != 1 || !strings.HasPrefix(c[0], "cycle: t0 → t99999 → t99998") {
			t.Fatalf("got %d cycles", len(c))
		}
	})
	// Dependents is one generation and the refusal only counts the subtree,
	// so both are linear even on a 100k chain; so is the cascade.
	long := chain(100_000, Todo)
	within(t, "dependents, drop refusal and cascade on a 100k chain", 2*time.Second, func() {
		if d := long.Dependents("t0", now); len(d) != 1 || d[0].ID != "t1" {
			t.Fatalf("got %+v", d)
		}
		_, err := long.Drop("t0", "gone", nil, now)
		var r *DropRefusal
		if !errors.As(err, &r) || r.Transitive != 99_998 {
			t.Fatalf("got %v", err)
		}
		res, err := long.DropCascade("t0", "gone", now)
		if err != nil || len(res.Dropped) != 100_000 {
			t.Fatalf("err %v, dropped %d", err, len(res.Dropped))
		}
	})
}

func TestScaleWideFan(t *testing.T) {
	tasks := []Task{task("root", Done)}
	for i := range 50_000 {
		tasks = append(tasks, task(fmt.Sprintf("leaf%d", i), Todo, "root"))
	}
	g := graphOf(tasks...)
	// 50k wide so a per-dependent linear scan (quadratic) blows the bound by
	// an order of magnitude.
	within(t, "validate, ready and dependents on a 50k fan", 5*time.Second, func() {
		mustValid(t, g)
		if r := g.Ready(now); len(r) != 50_000 {
			t.Fatalf("ready %d", len(r))
		}
		if d := g.Dependents("root", now); len(d) != 50_000 {
			t.Fatalf("dependents %d", len(d))
		}
	})
	within(t, "refuse, then cascade-drop, a copy of the 50k fan", 2*time.Second, func() {
		c := &Graph{Version: g.Version, Tasks: cloneTasks(g.Tasks)}
		_, err := c.Drop("root", "gone", nil, now)
		var r *DropRefusal
		if !errors.As(err, &r) || len(r.Dependents) != 50_000 || r.Transitive != 0 {
			t.Fatalf("got %v", err)
		}
		if res, err := c.DropCascade("root", "gone", now); err != nil || len(res.Dropped) != 50_001 {
			t.Fatalf("err %v", err)
		}
	})
	within(t, "drop the root of a 50k fan onto a replacement", 2*time.Second, func() {
		g.Tasks = append(g.Tasks, task("next", Done))
		res, err := g.Drop("root", "replaced", []string{"next"}, now)
		if err != nil || len(res.Rewired) != 50_000 {
			t.Fatalf("err %v, rewired %d", err, len(res.Rewired))
		}
	})
}

func TestScaleBinaryTree(t *testing.T) {
	// Node i needs its parent (i-1)/2: 1k tasks, depth 10.
	tasks := make([]Task, 1_000)
	for i := range tasks {
		var needs []string
		if i > 0 {
			needs = []string{fmt.Sprintf("n%d", (i-1)/2)}
		}
		tasks[i] = task(fmt.Sprintf("n%d", i), Todo, needs...)
	}
	g := graphOf(tasks...)
	within(t, "validate, dependents, split on a 1k tree", 2*time.Second, func() {
		mustValid(t, g)
		if d := g.Dependents("n0", now); len(d) != 2 {
			t.Fatalf("dependents %d", len(d))
		}
		g.Tasks = append(g.Tasks, task("half1", Todo), task("half2", Todo))
		if _, err := g.Split("n1", []string{"half1", "half2"}, now); err != nil {
			t.Fatal(err)
		}
		mustValid(t, g)
	})
}
