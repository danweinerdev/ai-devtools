package graph

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustDropRefusal(t *testing.T, err error) *DropRefusal {
	t.Helper()
	var r *DropRefusal
	if !errors.As(err, &r) {
		t.Fatalf("want a DropRefusal, got %v", err)
	}
	return r
}

func TestDropRefusalListsDirectDependentsOnly(t *testing.T) {
	// d needs c needs b needs a; d also needs a; e (done) needs a; x unrelated.
	g := graphOf(task("a", Done), task("b", Todo, "a"), task("x", Todo),
		task("c", Todo, "b"), task("d", Todo, "c", "a"), task("e", Done, "a"))
	_, err := g.Drop("a", "superseded", nil, now)
	r := mustDropRefusal(t, err)
	var got []string
	for _, d := range r.Dependents {
		got = append(got, fmt.Sprintf("%s:%s:%v", d.ID, d.Status, d.Blocks))
	}
	// Only the first generation; c reaches a only through b, so it's counted.
	if strings.Join(got, ",") != "b:todo:true,d:todo:true,e:done:false" || r.Transitive != 1 {
		t.Fatalf("dependents %v transitive %d", got, r.Transitive)
	}
	msg := r.Error()
	for _, s := range []string{"can't drop \"a\": 2 task(s) with work ahead need it directly", "b (todo)  [blocks]",
		"e (done)  [done: keeps its need as history]", "1 more task(s)", "--replaced-by", "--cascade"} {
		if !strings.Contains(msg, s) {
			t.Errorf("message lacks %q:\n%s", s, msg)
		}
	}
	if strings.Contains(msg, "→") || strings.Contains(msg, "c (") {
		t.Errorf("the refusal shows more than the first generation:\n%s", msg)
	}
	if g.Tasks[0].State.Status != Done {
		t.Fatal("a refused drop changed the task")
	}
}

func TestDropRefusalTransitiveCount(t *testing.T) {
	cases := []struct {
		name  string
		g     *Graph
		want  int
		hints string
	}{
		// Diamond below the direct dependent: d is counted once.
		{"diamond", graphOf(task("a", Todo), task("b", Todo, "a"), task("c1", Todo, "b"),
			task("c2", Todo, "b"), task("d", Todo, "c1", "c2")), 3, "--cascade"},
		// A done direct dependent stops the walk: what's below it isn't counted.
		{"behind done", graphOf(task("a", Done), task("b", Todo, "a"), task("f", Done, "a"),
			task("g", Todo, "f")), 0, "drop it first"},
		// A dropped task in between hides what's below it.
		{"behind dropped", graphOf(task("a", Todo), task("b", Todo, "a"), task("x", Dropped, "b"),
			task("y", Todo, "x")), 0, "drop it first"},
		// A direct dependent also reachable through another isn't counted twice.
		{"direct and indirect", graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Todo, "a", "b")), 0, "drop it first"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := mustDropRefusal(t, func() error { _, err := c.g.Drop("a", "gone", nil, now); return err }())
			if r.Transitive != c.want || !strings.Contains(r.Error(), c.hints) {
				t.Fatalf("transitive %d want %d:\n%s", r.Transitive, c.want, r.Error())
			}
		})
	}
}

func TestDropThreeDeepChainBottomUp(t *testing.T) {
	g := graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Todo, "b"), task("d", Todo, "c"))
	r := mustDropRefusal(t, func() error { _, err := g.Drop("a", "gone", nil, now); return err }())
	if len(r.Dependents) != 1 || r.Dependents[0].ID != "b" || r.Transitive != 2 {
		t.Fatalf("refusal %+v", r)
	}
	// Drop from the leaf up; each succeeds once nothing live needs it.
	for _, id := range []string{"d", "c", "b", "a"} {
		if _, err := g.Drop(id, "gone", nil, now); err != nil {
			t.Fatalf("drop %s: %v", id, err)
		}
	}
	for _, tk := range g.Tasks {
		if tk.State.Status != Dropped || tk.State.Reason != "gone" {
			t.Fatalf("%s: %+v", tk.ID, tk.State)
		}
	}
	if p := g.Validate(); len(p) != 0 {
		t.Fatal(p)
	}
}

func droppedIDs(g *Graph) string {
	var ids []string
	for _, t := range g.Tasks {
		if t.State.Status == Dropped {
			ids = append(ids, t.ID)
		}
	}
	return strings.Join(ids, ",")
}

func TestDropCascade(t *testing.T) {
	cases := []struct {
		name               string
		g                  *Graph
		dropped, kept, all string
	}{
		{"chain", graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Todo, "b"), task("d", Todo, "c")),
			"a,b,c,d", "", "a,b,c,d"},
		{"diamond", graphOf(task("x", Todo), task("a", Todo), task("b", Todo, "a"), task("c", Todo, "a"),
			task("d", Todo, "b", "c")), "a,b,c,d", "", "a,b,c,d"},
		{"fan", graphOf(task("a", Todo), task("l1", Todo, "a"), task("l2", Blocked, "a"), lapsed("l3", "a")),
			"a,l1,l2,l3", "", "a,l1,l2,l3"},
		// The done task in the middle stops the cascade and keeps its need;
		// what needs it still needs a done task, so stays.
		{"done in the middle", graphOf(task("a", Todo), task("b", Todo, "a"), task("m", Done, "a"),
			task("n", Todo, "m"), task("o", Todo, "b")), "a,b,o", "m", "a,b,o"},
		// The root is listed first, then the rest in file order.
		{"root later in file", graphOf(task("b", Todo, "a"), task("a", Todo), task("c", Todo, "b")),
			"a,b,c", "", "b,a,c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := c.g.DropCascade("a", "scrapped", now)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(res.Dropped, ",") != c.dropped || strings.Join(res.KeptAsHistory, ",") != c.kept {
				t.Fatalf("result %+v", res)
			}
			if droppedIDs(c.g) != c.all {
				t.Fatalf("dropped in the graph: %s", droppedIDs(c.g))
			}
			for _, tk := range c.g.Tasks {
				if tk.State.Status == Dropped && (tk.State.Reason != "scrapped" || !tk.State.Claim.IsZero()) {
					t.Fatalf("%s: %+v", tk.ID, tk.State)
				}
			}
			mustValid(t, c.g)
			for _, r := range c.g.Ready(now) {
				if r.State.Status == Dropped {
					t.Fatalf("%s is dropped but ready", r.ID)
				}
			}
		})
	}
}

func TestDropCascadeRefusals(t *testing.T) {
	live := func(id string, needs ...string) Task {
		tk := task(id, InProgress, needs...)
		tk.State.Claim = Claim{Holder: "mechanic", Expires: now.Add(time.Hour)}
		return tk
	}
	cases := []struct {
		name, reason, want string
		g                  *Graph
	}{
		{"live claim deep in the subtree", "gone", "d (claimed by \"mechanic\"",
			graphOf(task("a", Done), task("b", Done, "a"), task("c", Todo, "a"), live("d", "a"))},
		{"live claim on the root", "gone", "a (claimed by",
			graphOf(live("a"), task("b", Todo, "a"))},
		{"already dropped", "gone", "already dropped", graphOf(task("a", Dropped))},
		{"no reason", " ", "needs a reason", graphOf(task("a", Todo))},
		{"unknown", "gone", "no task", graphOf(task("b", Todo))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := cloneTasks(c.g.Tasks)
			_, err := c.g.DropCascade("a", c.reason, now)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
			if !reflect.DeepEqual(before, c.g.Tasks) {
				t.Fatal("a refused cascade changed the graph")
			}
		})
	}
}

func TestDropCascadeClearsLapsedClaims(t *testing.T) {
	g := graphOf(task("a", Todo), lapsed("b", "a"), task("c", Todo, "b"))
	res, err := g.DropCascade("a", "gone", now)
	if err != nil || strings.Join(res.Dropped, ",") != "a,b,c" {
		t.Fatalf("err %v result %+v", err, res)
	}
	if b, _ := g.ByID("b"); !b.State.Claim.IsZero() || b.State.Status != Dropped {
		t.Fatalf("b: %+v", b.State)
	}
}

func TestDropDoneDependentKeepsNeedAsHistory(t *testing.T) {
	g := graphOf(task("a", Done), task("b", Done, "a"), task("c", Todo, "b"))
	res, err := g.Drop("a", "no longer applies", nil, now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := g.ByID("b")
	if strings.Join(b.Needs, ",") != "a" || strings.Join(res.KeptAsHistory, ",") != "b" || len(res.Rewired) != 0 {
		t.Fatalf("b needs %v, result %+v", b.Needs, res)
	}
	if res.Task.State.Status != Dropped {
		t.Fatalf("task %+v", res.Task)
	}
	if p := g.Validate(); len(p) != 0 {
		t.Fatal(p)
	}
}

func TestDropReplacedByRewiresLiveDirectDependents(t *testing.T) {
	g := graphOf(task("a", Done), task("r1", Done), task("r2", Todo),
		task("b", Todo, "x0", "a", "r1"), task("c", Done, "a"), task("d", Todo, "b"), task("x0", Done))
	// x0 is defined after b; validation doesn't care about order.
	res, err := g.Drop("a", "split", []string{"r1", "r2", "r1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := g.ByID("b")
	c, _ := g.ByID("c")
	d, _ := g.ByID("d")
	if strings.Join(b.Needs, ",") != "x0,r1,r2" {
		t.Fatalf("b needs %v: replaced in place, deduped", b.Needs)
	}
	if strings.Join(c.Needs, ",") != "a" || strings.Join(d.Needs, ",") != "b" {
		t.Fatalf("done c and transitive d must be untouched: %v %v", c.Needs, d.Needs)
	}
	if strings.Join(res.Rewired, ",") != "b" || strings.Join(res.KeptAsHistory, ",") != "c" {
		t.Fatalf("result %+v", res)
	}
}

func TestDropReplacedByRejects(t *testing.T) {
	base := func() *Graph {
		return graphOf(task("a", Todo), task("b", Todo, "a"), task("old", Dropped), task("r", Todo, "b"))
	}
	cases := []struct {
		name string
		with []string
		want string
	}{
		{"missing replacement", []string{"zz"}, `replacement "zz" doesn't exist`},
		{"dropped replacement", []string{"old"}, `replacement "old" is dropped`},
		{"itself", []string{"a"}, `can't replace itself`},
		{"dependent as replacement", []string{"b"}, `replacement "b" needs "a" itself`},
		{"cycle", []string{"r"}, "cycle: b → r → b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := base()
			_, err := g.Drop("a", "x", c.with, now)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

func TestDropClaimsAndRefusals(t *testing.T) {
	g := graphOf(task("a", InProgress), task("b", Dropped), task("c", Todo))
	if _, err := g.Drop("a", "x", nil, now); err == nil || !strings.Contains(err.Error(), `claimed by "aid-mechanic"`) {
		t.Fatalf("live claim: %v", err)
	}
	// Once the lease lapses, the drop goes ahead and clears the claim.
	res, err := g.Drop("a", "x", nil, now.Add(2*time.Hour))
	if err != nil || !res.Task.State.Claim.IsZero() || res.Task.State.Status != Dropped {
		t.Fatalf("lapsed claim: %v %+v", err, res.Task.State)
	}
	for _, c := range []struct{ id, reason, want string }{
		{"b", "x", "already dropped"},
		{"c", " ", "needs a reason"},
		{"zz", "x", "zz"},
	} {
		if _, err := g.Drop(c.id, c.reason, nil, now); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("drop %s: want %q, got %v", c.id, c.want, err)
		}
	}
}

func TestDropLeavesFrontierAndBatch(t *testing.T) {
	g := graphOf(task("a", Todo), task("b", Todo))
	if _, err := g.Drop("a", "x", nil, now); err != nil {
		t.Fatal(err)
	}
	if got := ids(g.Ready(now)); got != "b" {
		t.Fatalf("ready %q", got)
	}
	if got := ids(g.Touching([]string{"internal/a.go"})); got != "" {
		t.Fatalf("a dropped task left in the batch selection: %q", got)
	}
}

func TestDropRefusalLeavesGraphUntouched(t *testing.T) {
	// a is needed by b and c (both todo); r needs b, so moving b's need onto r
	// would make a cycle.
	fresh := func() *Graph {
		return graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Todo, "a"), task("r", Todo, "b"), task("s", Todo))
	}
	cases := []struct {
		name       string
		reason     string
		replacedBy []string
		want       string
	}{
		{"no reason", "", nil, "needs a reason"},
		{"live dependents", "gone", nil, "with work ahead need it directly"},
		{"replacement is a direct dependent", "gone", []string{"s", "c"}, `replacement "c" needs "a" itself`},
		{"rewiring makes a cycle", "gone", []string{"r"}, "would leave the graph invalid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := fresh()
			before := cloneTasks(g.Tasks)
			_, err := g.Drop("a", c.reason, c.replacedBy, now)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want an error containing %q, got %v", c.want, err)
			}
			if !reflect.DeepEqual(before, g.Tasks) {
				t.Fatalf("a refused drop changed the graph:\nbefore %+v\nafter  %+v", before, g.Tasks)
			}
		})
	}
}
