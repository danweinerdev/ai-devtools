package graph

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func strp(s string) *string       { return &s }
func listp(s ...string) *[]string { return &s }

func mustValid(t *testing.T, g *Graph) {
	t.Helper()
	if p := g.Validate(); len(p) > 0 {
		t.Fatalf("graph invalid after edit: %q", p)
	}
}

func wantEditErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error containing %q, got %v", want, err)
	}
}

func TestUpdateChangesOnlyGivenFields(t *testing.T) {
	g := graphOf(task("a", Done), task("b", Todo))
	before := g.Tasks[1]
	got, err := g.UpdateTask("b", Edit{Title: strp("new title"), Checks: listp("go vet ./x", "go test ./internal/b")}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "new title" || !slices.Equal(got.Done.Checks, []string{"go vet ./x", "go test ./internal/b"}) {
		t.Fatalf("not applied: %+v", got)
	}
	if got.Done.Result != before.Done.Result || !slices.Equal(got.Footprint, before.Footprint) ||
		!slices.Equal(got.Needs, before.Needs) || got.State != before.State || got.History != nil {
		t.Fatalf("untouched fields changed: %+v", got)
	}
	if g.Tasks[1].Title != "new title" {
		t.Fatal("graph not updated in place")
	}
	if _, err := g.UpdateTask("a", Edit{Needs: listp(), Footprint: listp("cmd/aid/"), Result: strp("r"), By: "main", Reason: "reworded"}, now); err != nil {
		t.Fatal(err)
	}
	mustValid(t, g)
}

func TestUpdateRefusals(t *testing.T) {
	g := graphOf(task("a", Todo), task("d", Dropped))
	_, err := g.UpdateTask("d", Edit{Title: strp("x")}, now)
	wantEditErr(t, err, "is dropped")
	_, err = g.UpdateTask("zz", Edit{Title: strp("x")}, now)
	wantEditErr(t, err, `no task "zz"`)
	_, err = g.UpdateTask("a", Edit{}, now)
	wantEditErr(t, err, "nothing to update")
}

// A live claim freezes the task for everyone, its holder included, so the
// definition of done can't be swapped under the holder (or by it). Once the
// lease lapses the task is editable again.
func TestUpdateRefusesALiveClaim(t *testing.T) {
	edits := map[string]Edit{
		"title":     {Title: strp("x")},
		"needs":     {Needs: listp()},
		"footprint": {Footprint: listp("x")},
		"result":    {Result: strp("x")},
		"checks":    {Checks: listp("true")},
	}
	for name, e := range edits {
		t.Run(name, func(t *testing.T) {
			g := graphOf(task("a", InProgress))
			before := cloneTasks(g.Tasks)
			_, err := g.UpdateTask("a", e, now)
			wantEditErr(t, err, `task "a" is held by "aid-mechanic" until 2026-10-27T13:00:00Z; release it first, then edit it`)
			unchanged(t, g, before)
		})
	}
	g := graphOf(task("a", InProgress))
	if _, err := g.UpdateTask("a", Edit{Title: strp("x")}, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("a lapsed claim should be editable: %v", err)
	}
}

// Adding checks only adds proof. Removing or changing one, or changing the
// result, weakens the definition of done: refused without a reason, and
// recorded in the history with one, whatever the task's status.
func TestUpdateWeakeningNeedsAReasonAndIsRecorded(t *testing.T) {
	keep := "go test ./internal/a"
	cases := []struct {
		name string
		e    Edit
		want Weakening
	}{
		{"add a check", Edit{Checks: listp("go vet ./a", keep)}, Weakening{}},
		{"same result", Edit{Result: strp("a works")}, Weakening{}},
		{"replace the check", Edit{Checks: listp("true")}, Weakening{Removed: []string{keep}}},
		{"reword the check", Edit{Checks: listp(keep + " -count=1")}, Weakening{Removed: []string{keep}}},
		{"change the result", Edit{Result: strp("a compiles")}, Weakening{Result: true}},
		{"both", Edit{Checks: listp("true"), Result: strp("x")}, Weakening{Removed: []string{keep}, Result: true}},
	}
	for _, c := range cases {
		for _, status := range []string{Todo, Done} {
			t.Run(c.name+"/"+status, func(t *testing.T) {
				g := graphOf(task("a", status))
				if got := WeakeningOf(g.Tasks[0], c.e); !reflect.DeepEqual(got, c.want) {
					t.Fatalf("WeakeningOf = %+v, want %+v", got, c.want)
				}
				before := cloneTasks(g.Tasks)
				_, err := g.UpdateTask("a", c.e, now)
				if !c.want.Weakens() {
					if err != nil || g.Tasks[0].History != nil {
						t.Fatalf("a strengthening edit: err %v, history %+v", err, g.Tasks[0].History)
					}
					return
				}
				wantEditErr(t, err, "weakens its definition of done; say why with --reason")
				unchanged(t, g, before)

				e := c.e
				e.By, e.Reason = "aid-mechanic", "  the old check tested the wrong package  "
				got, err := g.UpdateTask("a", e, now)
				if err != nil {
					t.Fatal(err)
				}
				want := Change{At: now, By: "aid-mechanic", Reason: "the old check tested the wrong package"}
				if c.want.Removed != nil {
					want.ChecksFrom, want.ChecksTo = []string{keep}, *e.Checks
				}
				if c.want.Result {
					want.ResultFrom, want.ResultTo = "a works", *e.Result
				}
				if len(got.History) != 1 || !reflect.DeepEqual(got.History[0], want) {
					t.Fatalf("history %+v, want %+v", got.History, want)
				}
				mustValid(t, g)
			})
		}
	}
}

func TestHistoryValidation(t *testing.T) {
	good := Change{At: now, By: "main", Reason: "r", ChecksFrom: []string{"a"}, ChecksTo: []string{"b"}}
	cases := []struct {
		name string
		edit func(*Change)
		want string
	}{
		{"no time", func(c *Change) { c.At = time.Time{} }, "history[0] has no time"},
		{"no author", func(c *Change) { c.By = " " }, "history[0] says nothing about who made it"},
		{"no reason", func(c *Change) { c.Reason = "" }, "history[0] gives no reason"},
		{"no change", func(c *Change) { c.ChecksFrom, c.ChecksTo = nil, nil }, "history[0] records no change"},
		{"half the checks", func(c *Change) { c.ChecksTo = nil }, "history[0] needs both `checks_from` and `checks_to`"},
		{"half the result", func(c *Change) { c.ChecksFrom, c.ChecksTo, c.ResultFrom = nil, nil, "x" }, "history[0] needs both `result_from` and `result_to`"},
	}
	g := graphOf(task("a", Todo))
	g.Tasks[0].History = []Change{good}
	mustValid(t, g)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := good
			c.edit(&ch)
			g := graphOf(task("a", Todo))
			g.Tasks[0].History = []Change{ch}
			if p := strings.Join(g.Validate(), "\n"); !strings.Contains(p, "task `a`: "+c.want) {
				t.Fatalf("got %q, want %q", p, c.want)
			}
		})
	}
}

func TestUpdateStructuralBreakageCaughtByValidate(t *testing.T) {
	g := graphOf(task("a", Todo), task("b", Todo, "a"))
	_, err := g.UpdateTask("a", Edit{Needs: listp("b")}, now)
	wantEditErr(t, err, "cycle: a → b → a")
	if len(g.Tasks[0].Needs) != 0 {
		t.Fatal("a refused update changed the graph")
	}
	g = graphOf(task("a", Todo), task("b", Done))
	_, err = g.UpdateTask("b", Edit{Needs: listp("a")}, now)
	wantEditErr(t, err, "is done but needs `a`")
}

func TestSplitRewiresAndInherits(t *testing.T) {
	// base <- big <- (user1, user2); parts p1 (own need: x), p2.
	g := graphOf(task("base", Done), task("x", Done), task("big", Todo, "base"),
		task("p1", Todo, "x"), task("p2", Todo),
		task("user1", Todo, "x", "big"), task("user2", Todo, "big", "p2"),
		task("old", Dropped, "big"), task("other", Todo, "base"))
	changed, err := g.Split("big", []string{"p1", "p2"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(changed); got != "big,p1,p2,user1,user2" {
		t.Fatalf("changed %s", got)
	}
	by := func(id string) Task { tk, _ := g.ByID(id); return tk }
	if s := by("big").State; s.Status != Dropped || s.Reason != "split into p1, p2" || !s.Claim.IsZero() {
		t.Fatalf("split task state %+v", s)
	}
	for id, want := range map[string][]string{
		"p1":    {"x", "base"},     // own first, then inherited
		"p2":    {"base"},          // inherited
		"user1": {"x", "p1", "p2"}, // replaced in place
		"user2": {"p1", "p2"},      // deduped against its own p2
		"old":   {"big"},           // dropped dependents are records, untouched
		"other": {"base"},          // unrelated
	} {
		if got := by(id).Needs; !slices.Equal(got, want) {
			t.Errorf("%s needs %v, want %v", id, got, want)
		}
	}
	mustValid(t, g)
}

func TestSplitLapsedClaimAndBlockedAllowed(t *testing.T) {
	g := graphOf(task("big", InProgress), task("p", Todo))
	if _, err := g.Split("big", []string{"p"}, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("a lapsed lease should not block a split: %v", err)
	}
	mustValid(t, g)
	g = graphOf(task("big", Blocked), task("p", Todo))
	if _, err := g.Split("big", []string{"p"}, now); err != nil {
		t.Fatal(err)
	}
	if g.Tasks[0].State.Reason != "split into p" {
		t.Fatalf("reason %q", g.Tasks[0].State.Reason)
	}
}

func TestSplitRefusals(t *testing.T) {
	cases := []struct {
		name  string
		g     *Graph
		parts []string
		want  string
	}{
		{"done", graphOf(task("big", Done), task("p", Todo)), []string{"p"}, "is done"},
		{"dropped", graphOf(task("big", Dropped), task("p", Todo)), []string{"p"}, "is dropped"},
		{"live claim", graphOf(task("big", InProgress), task("p", Todo)), []string{"p"}, `claimed by "aid-mechanic"`},
		{"missing task", graphOf(task("p", Todo)), []string{"p"}, `no task "big"`},
		{"no parts", graphOf(task("big", Todo)), nil, "at least one part"},
		{"missing part", graphOf(task("big", Todo)), []string{"nope"}, `part "nope" doesn't exist`},
		{"self part", graphOf(task("big", Todo)), []string{"big"}, "its own parts"},
		{"duplicate part", graphOf(task("big", Todo), task("p", Todo)), []string{"p", "p"}, "listed twice"},
		{"part not todo", graphOf(task("big", Todo), task("p", Done)), []string{"p"}, `part "p" is done`},
		{"part needs the split task", graphOf(task("big", Todo), task("p", Todo, "big")), []string{"p"}, "can't depend on what it replaces"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snapshot := slices.Clone(c.g.Tasks)
			_, err := c.g.Split("big", c.parts, now)
			wantEditErr(t, err, c.want)
			for i := range snapshot {
				if c.g.Tasks[i].State != snapshot[i].State || !slices.Equal(c.g.Tasks[i].Needs, snapshot[i].Needs) {
					t.Fatalf("a refused split changed the graph")
				}
			}
		})
	}
}

func TestSplitCycleRejectedByValidate(t *testing.T) {
	// p transitively depends on a dependent of big: user needs big, p needs user.
	// After the split, user needs p and p needs user: a cycle.
	g := graphOf(task("big", Todo), task("user", Todo, "big"), task("p", Todo, "user"))
	before := cloneTasks(g.Tasks)
	_, err := g.Split("big", []string{"p"}, now)
	wantEditErr(t, err, "cycle: user → p → user")
	if !reflect.DeepEqual(g.Tasks, before) {
		t.Fatal("a refused split changed the graph")
	}
}
