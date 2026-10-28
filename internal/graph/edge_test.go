package graph

import (
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Edge cases of the dependency tree. Each test states input → operation →
// expected outcome; every refusal also leaves the graph deep-equal.

func unchanged(t *testing.T, g *Graph, before []Task) {
	t.Helper()
	if !reflect.DeepEqual(g.Tasks, before) {
		t.Fatal("a refused operation changed the graph")
	}
}

func lapsed(id string, needs ...string) Task {
	x := task(id, InProgress, needs...)
	x.State.Claim.Expires = now.Add(-time.Minute)
	return x
}

// G2: a split part can't be one of the split task's prerequisites.
func TestSplitRefusesPrerequisiteParts(t *testing.T) {
	cases := []struct {
		name  string
		g     *Graph
		parts []string
	}{
		// big needs base; split big into [base] would make base need itself.
		{"direct", graphOf(task("base", Todo), task("big", Todo, "base")), []string{"base"}},
		// big needs mid, mid needs p; split big into [p] would loop p → mid → p.
		{"through a chain", graphOf(task("p", Todo), task("mid", Todo, "p"), task("big", Todo, "mid")), []string{"p"}},
		// big needs c, c needs b, b needs a; a is an ancestor three levels up.
		{"deep ancestor", graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Todo, "b"),
			task("big", Todo, "c"), task("q", Todo)), []string{"q", "a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := cloneTasks(c.g.Tasks)
			_, err := c.g.Split("big", c.parts, now)
			wantEditErr(t, err, "is a prerequisite of \"big\"")
			unchanged(t, c.g, before)
		})
	}
}

func TestSplitPartsMayNeedEachOther(t *testing.T) {
	// big; user needs big; p2 needs p1. Split big into [p1, p2] → user needs both.
	g := graphOf(task("big", Todo), task("user", Todo, "big"), task("p1", Todo), task("p2", Todo, "p1"))
	if _, err := g.Split("big", []string{"p1", "p2"}, now); err != nil {
		t.Fatal(err)
	}
	mustValid(t, g)
	if u, _ := g.ByID("user"); strings.Join(u.Needs, ",") != "p1,p2" {
		t.Fatalf("user needs %v", u.Needs)
	}
}

// G3: Dependents, directly.
func TestDependentsAreTheFirstGenerationInFileOrder(t *testing.T) {
	// a ← b, a ← c, d needs b and c: only b and c need a directly.
	g := graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Todo, "a"), task("d", Todo, "b", "c"))
	if got := idsOf(g.Dependents("a", now)); got != "b,c" {
		t.Fatalf("got %s", got)
	}
	g = graphOf(task("a", Todo), task("c", Todo, "a"), task("b", Todo, "a"), task("d", Todo, "b", "c"))
	if got := idsOf(g.Dependents("a", now)); got != "c,b" {
		t.Fatalf("got %s", got)
	}
}

func idsOf(deps []Dependent) string {
	var ids []string
	for _, d := range deps {
		ids = append(ids, d.ID)
	}
	return strings.Join(ids, ",")
}

func TestDependentsSkipDroppedIntermediates(t *testing.T) {
	// a ← x (dropped) ← y: x is dropped and y doesn't need a, so neither is a dependent.
	g := graphOf(task("a", Todo), task("x", Dropped, "a"), task("y", Todo, "x"))
	if deps := g.Dependents("a", now); len(deps) != 0 {
		t.Fatalf("got %+v", deps)
	}
}

func TestDependentsFanOutAllBlock(t *testing.T) {
	tasks := []Task{task("root", Todo)}
	for i := range 50 {
		tasks = append(tasks, task(fmt.Sprintf("leaf%02d", i), Todo, "root"))
	}
	deps := graphOf(tasks...).Dependents("root", now)
	if len(deps) != 50 {
		t.Fatalf("got %d dependents", len(deps))
	}
	for i, d := range deps {
		if d.ID != fmt.Sprintf("leaf%02d", i) || !d.Blocks {
			t.Fatalf("dependent %d: %+v", i, d)
		}
	}
}

func TestDependentsEveryWorkAheadStatusBlocks(t *testing.T) {
	// Direct dependents that are in-progress (live), blocked, and in-progress with a
	// lapsed lease (effectively todo) all block; a done one doesn't.
	g := graphOf(task("a", Done), task("live", InProgress, "a"), task("stuck", Blocked, "a"),
		lapsed("stale", "a"), task("fin", Done, "a"))
	want := map[string]struct {
		status string
		blocks bool
	}{"live": {InProgress, true}, "stuck": {Blocked, true}, "stale": {Todo, true}, "fin": {Done, false}}
	deps := g.Dependents("a", now)
	if len(deps) != len(want) {
		t.Fatalf("got %+v", deps)
	}
	for _, d := range deps {
		if w := want[d.ID]; d.Status != w.status || d.Blocks != w.blocks {
			t.Fatalf("%s: got %+v want %+v", d.ID, d, w)
		}
	}
}

// G4/G5: an in-progress task never gains a need that isn't done.
func TestDoingTaskNeverGainsAnUnfinishedNeed(t *testing.T) {
	t.Run("drop --replaced-by", func(t *testing.T) {
		// a done; b in-progress needs a; drop a replaced by c (todo) would strand b.
		g := graphOf(task("a", Done), task("b", InProgress, "a"), task("c", Todo))
		before := cloneTasks(g.Tasks)
		_, err := g.Drop("a", "obsolete", []string{"c"}, now)
		wantEditErr(t, err, "task `b`: is in-progress but needs `c`, which is todo; release it before rewiring its needs")
		unchanged(t, g, before)
	})
	t.Run("update-task", func(t *testing.T) {
		// A live claim refuses every edit; a lapsed one is still in-progress
		// on disk, so validation must catch the rewiring.
		g := graphOf(task("x", InProgress), task("y", Todo))
		before := cloneTasks(g.Tasks)
		_, err := g.UpdateTask("x", Edit{Needs: listp("y")}, now.Add(2*time.Hour))
		wantEditErr(t, err, "task `x`: is in-progress but needs `y`, which is todo")
		unchanged(t, g, before)
	})
	t.Run("validate", func(t *testing.T) {
		// An in-progress dependent of a task that isn't done can't exist, which is
		// why a split (only of tasks that aren't done) never rewires one.
		g := graphOf(task("big", Todo), task("w", InProgress, "big"))
		if p := strings.Join(g.Validate(), "\n"); !strings.Contains(p, "is in-progress but needs `big`, which is todo") {
			t.Fatalf("got %s", p)
		}
		g = graphOf(task("gone", Dropped), task("w", InProgress, "gone"))
		if p := strings.Join(g.Validate(), "\n"); !strings.Contains(p, "needs `gone`, which is dropped") {
			t.Fatalf("got %s", p)
		}
	})
	t.Run("drop --replaced-by onto a done task is fine", func(t *testing.T) {
		g := graphOf(task("a", Done), task("b", InProgress, "a"), task("c", Done))
		if _, err := g.Drop("a", "obsolete", []string{"c"}, now); err != nil {
			t.Fatal(err)
		}
		mustValid(t, g)
	})
}

// G8: update-task's structural refusals, with exact messages.
func TestUpdateRefusesStructuralBreakage(t *testing.T) {
	base := func() *Graph {
		return graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Todo, "b"), task("x", Dropped))
	}
	cases := []struct {
		name, id string
		needs    []string
		want     string
	}{
		{"three-cycle", "a", []string{"c"}, "cycle: a → c → b → a"},
		{"self need", "a", []string{"a"}, "task `a`: needs itself"},
		{"dropped need", "a", []string{"x"}, "task `a`: needs `x`, which is dropped; move or remove the need"},
		{"missing need", "a", []string{"ghost"}, "task `a`: needs `ghost`, which doesn't exist"},
		{"duplicate need", "c", []string{"b", "b"}, "task `c`: needs `b` twice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := base()
			before := cloneTasks(g.Tasks)
			_, err := g.UpdateTask(c.id, Edit{Needs: &c.needs}, now)
			wantEditErr(t, err, c.want)
			unchanged(t, g, before)
		})
	}
}

func cyclesOf(g *Graph) []string {
	var out []string
	for _, p := range g.Validate() {
		if strings.HasPrefix(p, "cycle: ") {
			out = append(out, p)
		}
	}
	return out
}

// G9: cycle reporting.
func TestValidateCycleShapes(t *testing.T) {
	t.Run("self need is only 'needs itself'", func(t *testing.T) {
		if p := graphOf(task("a", Todo, "a")).Validate(); len(p) != 1 || p[0] != "task `a`: needs itself" {
			t.Fatalf("got %q", p)
		}
	})
	t.Run("loops sharing tasks are one knot", func(t *testing.T) {
		// a needs b and c; b and c need d; d needs a: two loops, one knot.
		g := graphOf(task("a", Todo, "b", "c"), task("b", Todo, "d"), task("c", Todo, "d"), task("d", Todo, "a"))
		if got := cyclesOf(g); len(got) != 1 || got[0] != "cycle: a → b → d → a (also involves `c`)" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("through a duplicate-ID copy", func(t *testing.T) {
		// The second a needs b, and b needs a: the loop runs through the copy.
		g := graphOf(task("a", Todo), task("b", Todo, "a"), task("a", Todo, "b"))
		p := strings.Join(g.Validate(), "\n")
		if !strings.Contains(p, "task `a`: duplicate id") || !strings.Contains(p, "cycle: a → b → a") {
			t.Fatalf("got %s", p)
		}
	})
	t.Run("among done and dropped tasks", func(t *testing.T) {
		g := graphOf(task("a", Done, "b"), task("b", Dropped, "a"))
		if got := cyclesOf(g); len(got) != 1 || got[0] != "cycle: a → b → a" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("disjoint loops each once, in file order", func(t *testing.T) {
		g := graphOf(task("p", Todo, "q"), task("x", Todo, "y"), task("q", Todo, "p"),
			task("y", Todo, "z"), task("z", Todo, "x"), task("m", Todo, "m2"), task("m2", Todo, "m"))
		want := []string{"cycle: p → q → p", "cycle: x → y → z → x", "cycle: m → m2 → m"}
		if got := cyclesOf(g); !slices.Equal(got, want) {
			t.Fatalf("got %q want %q", got, want)
		}
	})
}

// G10: drop --replaced-by details.
func TestDropReplacementDetails(t *testing.T) {
	t.Run("replacement already needed is deduped", func(t *testing.T) {
		g := graphOf(task("a", Todo), task("r", Todo), task("b", Todo, "a", "r"))
		if _, err := g.Drop("a", "gone", []string{"r"}, now); err != nil {
			t.Fatal(err)
		}
		if b, _ := g.ByID("b"); strings.Join(b.Needs, ",") != "r" {
			t.Fatalf("b needs %v", b.Needs)
		}
	})
	t.Run("an ancestor of the dropped task may replace it", func(t *testing.T) {
		g := graphOf(task("z", Done), task("a", Todo, "z"), task("b", Todo, "a"))
		if _, err := g.Drop("a", "gone", []string{"z"}, now); err != nil {
			t.Fatal(err)
		}
		mustValid(t, g)
	})
	t.Run("the dropped task's own needs aren't inherited", func(t *testing.T) {
		g := graphOf(task("z", Todo), task("a", Todo, "z"), task("r", Todo), task("b", Todo, "a"))
		if _, err := g.Drop("a", "gone", []string{"r"}, now); err != nil {
			t.Fatal(err)
		}
		if b, _ := g.ByID("b"); strings.Join(b.Needs, ",") != "r" {
			t.Fatalf("b needs %v; drop doesn't pass a's needs on, unlike split", b.Needs)
		}
	})
}

// G11: claim agrees with Ready on needs that aren't done.
func TestClaimAgreesWithReady(t *testing.T) {
	cases := []struct {
		name string
		need Task
		want string
	}{
		{"lapsed in-progress need", lapsed("a"), "it needs a (todo)"},
		{"blocked need", task("a", Blocked), "it needs a (blocked)"},
		{"missing need", task("other", Done), "it needs a (missing)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := graphOf(c.need, task("b", Todo, "a"))
			if slices.ContainsFunc(g.Ready(now), func(x Task) bool { return x.ID == "b" }) {
				t.Fatal("b is ready")
			}
			_, err := g.Claim("b", "h", time.Hour, now)
			wantEditErr(t, err, c.want)
		})
	}
}

// G12: every listing follows file order, whatever order the file is in.
func TestOrderFollowsFileOrderUnderShuffles(t *testing.T) {
	base := []Task{
		task("root", Done), task("a", Todo, "root"), task("b", Todo, "root"), task("c", Todo, "a"),
		task("d", Todo, "b", "a"), task("e", Todo), task("big", Todo, "root"), task("u1", Todo, "big"),
		task("u2", Todo, "big"), task("p1", Todo), task("p2", Todo), task("x", Todo, "y"),
		task("y", Todo, "z"), task("z", Todo, "x"),
	}
	inFileOrder := func(g *Graph, got []string) bool {
		pos := g.positions()
		return slices.IsSortedFunc(got, func(a, b string) int { return pos[a] - pos[b] })
	}
	for seed := int64(1); seed <= 40; seed++ {
		r := rand.New(rand.NewSource(seed))
		tasks := cloneTasks(base)
		r.Shuffle(len(tasks), func(i, j int) { tasks[i], tasks[j] = tasks[j], tasks[i] })
		g := graphOf(tasks...)

		var ready, deps []string
		for _, x := range g.Ready(now) {
			ready = append(ready, x.ID)
		}
		for _, d := range g.Dependents("root", now) {
			deps = append(deps, d.ID)
		}
		if !inFileOrder(g, ready) || !inFileOrder(g, deps) {
			t.Fatalf("seed %d: ready %v deps %v not in file order", seed, ready, deps)
		}

		// The cycle report starts at whichever of x, y, z comes first.
		first := ""
		for _, x := range g.Tasks {
			if x.ID == "x" || x.ID == "y" || x.ID == "z" {
				first = x.ID
				break
			}
		}
		if c := cyclesOf(g); len(c) != 1 || !strings.HasPrefix(c[0], "cycle: "+first+" → ") {
			t.Fatalf("seed %d: cycles %q should start at %s", seed, c, first)
		}

		// Split's changed list: split task first, then file order.
		g.Tasks = slices.DeleteFunc(g.Tasks, func(x Task) bool { return x.ID == "x" || x.ID == "y" || x.ID == "z" })
		out, err := g.Split("big", []string{"p2", "p1"}, now)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		var changed []string
		for _, x := range out[1:] {
			changed = append(changed, x.ID)
		}
		if out[0].ID != "big" || len(changed) != 4 || !inFileOrder(g, changed) {
			t.Fatalf("seed %d: changed %s,%v", seed, out[0].ID, changed)
		}
	}
}

// Footprint and Covers corner cases from the review.
func TestFootprintAdviceIsNeverRefused(t *testing.T) {
	if got := footprintProblem("a/../.."); got != "leaves the repository" {
		t.Fatalf("got %q", got)
	}
	if got := footprintProblem("a/./b"); got != `isn't clean; write it as "a/b"` {
		t.Fatalf("got %q", got)
	}
	if Covers("", "anything") {
		t.Fatal("an empty entry covers nothing")
	}
}
