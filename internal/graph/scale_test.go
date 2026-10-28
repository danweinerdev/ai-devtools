package graph

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// G6: big graphs stay correct and never recurse deeply, and every operation
// grows linearly. Speed is checked by growth, never by wall-clock bounds: an
// operation is timed at n and at 4n, and the ratio must stay under maxGrowth.
// Linear work comes out near 4x and quadratic near 16x, whatever the machine,
// the OS or the race detector, since their slowdown cancels in the ratio.

const maxGrowth = 10.0

// growth times build-then-op at n and 4n (best of three, after a GC, timing
// only op) and fails if the larger run is maxGrowth times slower or more.
func growth(t *testing.T, what string, n int, build func(n int) *Graph, op func(t *testing.T, g *Graph, n int)) {
	t.Helper()
	best := func(size int) time.Duration {
		var min time.Duration
		for range 3 {
			g := build(size)
			runtime.GC()
			start := time.Now()
			op(t, g, size)
			if d := time.Since(start); min == 0 || d < min {
				min = d
			}
		}
		return max(min, time.Microsecond)
	}
	small, large := best(n), best(4*n)
	ratio := float64(large) / float64(small)
	t.Logf("%s: %s at %d, %s at %d (%.1fx)", what, small, n, large, 4*n, ratio)
	if ratio >= maxGrowth {
		t.Fatalf("%s grows superlinearly: %s at %d, %s at %d (%.1fx; linear is ~4x, limit %.0fx)",
			what, small, n, large, 4*n, ratio, maxGrowth)
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
	mustValid(t, g) // validate a 100k chain
	// ready on a 100k chain
	if r := g.Ready(now); len(r) != 1 || r[0].ID != "t0" {
		t.Fatalf("ready %d", len(r))
	}
	// A 100k loop: the cycle check is iterative, so depth can't overflow.
	g.Tasks[0].Needs = []string{"t99999"}
	// find a 100k-long cycle
	if c := cyclesOf(g); len(c) != 1 || !strings.HasPrefix(c[0], "cycle: t0 → t99999 → t99998") {
		t.Fatalf("got %d cycles", len(c))
	}
	// Dependents is one generation and the refusal only counts the subtree,
	// so both are linear even on a 100k chain; so is the cascade.
	long := chain(100_000, Todo)
	// dependents, drop refusal and cascade on a 100k chain
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
}

func TestScaleWideFan(t *testing.T) {
	tasks := []Task{task("root", Done)}
	for i := range 50_000 {
		tasks = append(tasks, task(fmt.Sprintf("leaf%d", i), Todo, "root"))
	}
	g := graphOf(tasks...)
	// 50k wide so a per-dependent linear scan (quadratic) blows the bound by
	// an order of magnitude.
	// validate, ready and dependents on a 50k fan
	mustValid(t, g)
	if r := g.Ready(now); len(r) != 50_000 {
		t.Fatalf("ready %d", len(r))
	}
	if d := g.Dependents("root", now); len(d) != 50_000 {
		t.Fatalf("dependents %d", len(d))
	}
	// refuse, then cascade-drop, a copy of the 50k fan
	c := &Graph{Version: g.Version, Tasks: cloneTasks(g.Tasks)}
	_, err := c.Drop("root", "gone", nil, now)
	var r *DropRefusal
	if !errors.As(err, &r) || len(r.Dependents) != 50_000 || r.Transitive != 0 {
		t.Fatalf("got %v", err)
	}
	if res, err := c.DropCascade("root", "gone", now); err != nil || len(res.Dropped) != 50_001 {
		t.Fatalf("err %v", err)
	}
	// drop the root of a 50k fan onto a replacement
	g.Tasks = append(g.Tasks, task("next", Done))
	res, err := g.Drop("root", "replaced", []string{"next"}, now)
	if err != nil || len(res.Rewired) != 50_000 {
		t.Fatalf("err %v, rewired %d", err, len(res.Rewired))
	}
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
	// validate, dependents, split on a 1k tree
	mustValid(t, g)
	if d := g.Dependents("n0", now); len(d) != 2 {
		t.Fatalf("dependents %d", len(d))
	}
	g.Tasks = append(g.Tasks, task("half1", Todo), task("half2", Todo))
	if _, err := g.Split("n1", []string{"half1", "half2"}, now); err != nil {
		t.Fatal(err)
	}
	mustValid(t, g)
}

func fan(n int) *Graph {
	tasks := []Task{task("root", Done)}
	for i := range n {
		tasks = append(tasks, task(fmt.Sprintf("leaf%d", i), Todo, "root"))
	}
	return graphOf(tasks...)
}

func TestScaleGrowsLinearly(t *testing.T) {
	todoChain := func(n int) *Graph { return chain(n, Todo) }
	loop := func(n int) *Graph {
		g := chain(n, Todo)
		g.Tasks[0].Needs = []string{fmt.Sprintf("t%d", n-1)}
		return g
	}
	cases := []struct {
		what  string
		build func(int) *Graph
		op    func(*testing.T, *Graph, int)
	}{
		{"validate a chain", todoChain, func(t *testing.T, g *Graph, _ int) { mustValid(t, g) }},
		{"ready on a chain", todoChain, func(t *testing.T, g *Graph, _ int) { g.Ready(now) }},
		{"find a chain-long cycle", loop, func(t *testing.T, g *Graph, _ int) {
			if len(cyclesOf(g)) != 1 {
				t.Fatal("cycle not found")
			}
		}},
		{"drop refusal on a chain", todoChain, func(t *testing.T, g *Graph, _ int) {
			if _, err := g.Drop("t0", "gone", nil, now); err == nil {
				t.Fatal("not refused")
			}
		}},
		{"cascade a chain", todoChain, func(t *testing.T, g *Graph, n int) {
			if res, err := g.DropCascade("t0", "gone", now); err != nil || len(res.Dropped) != n {
				t.Fatalf("err %v", err)
			}
		}},
		{"validate, ready and dependents on a fan", fan, func(t *testing.T, g *Graph, n int) {
			mustValid(t, g)
			g.Ready(now)
			if len(g.Dependents("root", now)) != n {
				t.Fatal("dependents")
			}
		}},
		{"cascade a fan", fan, func(t *testing.T, g *Graph, n int) {
			if res, err := g.DropCascade("root", "gone", now); err != nil || len(res.Dropped) != n+1 {
				t.Fatalf("err %v", err)
			}
		}},
		{"drop a fan's root onto a replacement", func(n int) *Graph {
			g := fan(n)
			g.Tasks = append(g.Tasks, task("next", Done))
			return g
		}, func(t *testing.T, g *Graph, n int) {
			if res, err := g.Drop("root", "replaced", []string{"next"}, now); err != nil || len(res.Rewired) != n {
				t.Fatalf("err %v", err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) { growth(t, c.what, 12_500, c.build, c.op) })
	}
}
