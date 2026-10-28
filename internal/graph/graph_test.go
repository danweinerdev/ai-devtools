package graph

import (
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/ai-devtools/internal/store"
)

var now = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func task(id string, status string, needs ...string) Task {
	t := Task{
		ID:        id,
		Title:     "build " + id,
		Needs:     append([]string{}, needs...),
		Footprint: []string{"internal/" + id + ".go"},
		Done:      DoneWhen{Result: id + " works", Checks: []string{"go test ./internal/" + id}},
		State:     State{Status: status},
	}
	switch status {
	case InProgress:
		t.State.Claim = Claim{Holder: "aid-mechanic", Expires: now.Add(time.Hour)}
	case Blocked, Dropped:
		t.State.Reason = "because"
	}
	return t
}

func graphOf(tasks ...Task) *Graph {
	return &Graph{Version: Version, Tasks: tasks}
}

func ids(tasks []Task) string {
	var out []string
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return strings.Join(out, ",")
}

func TestValidateAcceptsAValidGraph(t *testing.T) {
	g := graphOf(task("a", Done), task("b", InProgress, "a"), task("c", Todo, "a", "b"),
		task("d", Blocked), task("e", Dropped, "d"), task("018f6b2e-7c1a-7d3e-9f00-0123456789ab", Todo))
	// a:b and 1:x have a colon but aren't drive roots.
	g.Tasks[0].Footprint = []string{"cmd/aid/", "internal/store/txn.go", "a:b", "1:x"}
	g.Tasks = append(g.Tasks, task("f", Done, "a", "e")) // a done task keeps its need on a dropped one as history
	if p := g.Validate(); len(p) != 0 {
		t.Fatalf("unexpected problems: %q", p)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(g *Graph)
		want   string
	}{
		{"bad id", func(g *Graph) { g.Tasks[0].ID = "Store_Core" }, "tasks[0]: id \"Store_Core\" must match"},
		{"empty id", func(g *Graph) { g.Tasks[0].ID = "" }, "tasks[0]: id \"\" must match"},
		{"duplicate id", func(g *Graph) { g.Tasks[1].ID = "a"; g.Tasks[1].Needs = nil }, "task `a`: duplicate id"},
		{"empty title", func(g *Graph) { g.Tasks[0].Title = " " }, "task `a`: title is empty"},
		{"empty result", func(g *Graph) { g.Tasks[0].Done.Result = "" }, "task `a`: done.result is empty"},
		{"no checks", func(g *Graph) { g.Tasks[0].Done.Checks = []string{} }, "task `a`: done.checks needs at least one command"},
		{"empty check", func(g *Graph) { g.Tasks[0].Done.Checks = []string{"go test", ""} }, "task `a`: done.checks[1] is empty"},
		{"no footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{} }, "task `a`: footprint needs at least one path"},
		{"absolute footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{"/etc/passwd"} }, "is absolute"},
		{"drive footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{"C:/x"} }, "is absolute"},
		{"bare drive footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{"d:"} }, "is absolute"},
		{"escaping footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{"../other"} }, "leaves the repository"},
		{"unclean footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{"./cmd/../cmd/aid"} }, "isn't clean"},
		{"backslash footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{`cmd\aid`} }, "uses backslashes"},
		{"empty footprint", func(g *Graph) { g.Tasks[0].Footprint = []string{""} }, "is empty"},
		{"self need", func(g *Graph) { g.Tasks[0].Needs = []string{"a"} }, "task `a`: needs itself"},
		{"duplicate need", func(g *Graph) { g.Tasks[1].Needs = []string{"a", "a"} }, "task `b`: needs `a` twice"},
		{"dangling need", func(g *Graph) { g.Tasks[1].Needs = []string{"zz"} }, "task `b`: needs `zz`, which doesn't exist"},
		{"unknown status", func(g *Graph) { g.Tasks[0].State.Status = "finished" }, "status \"finished\" must be one of"},
		{"in-progress without claim", func(g *Graph) { g.Tasks[1].State.Claim = Claim{} }, "task `b`: is in-progress but has no claim"},
		{"in-progress without expiry", func(g *Graph) { g.Tasks[1].State.Claim.Expires = time.Time{} }, "is in-progress but has no claim"},
		{"claim on todo", func(g *Graph) { g.Tasks[2].State.Claim = Claim{Holder: "x", Expires: now} }, "task `c`: is todo but carries a claim"},
		{"blocked without reason", func(g *Graph) { g.Tasks[3].State.Reason = "" }, "task `d`: is blocked without a reason"},
		{"dropped without reason", func(g *Graph) { g.Tasks[4].State.Reason = "" }, "task `e`: is dropped without a reason"},
		{"reason on todo", func(g *Graph) { g.Tasks[2].State.Reason = "x" }, "task `c`: is todo but carries a reason"},
		{"live task needs dropped", func(g *Graph) { g.Tasks[2].Needs = []string{"e"} }, "task `c`: needs `e`, which is dropped"},
		{"done needs undone", func(g *Graph) { g.Tasks[0].Needs = []string{"c"}; g.Tasks[2].Needs = nil }, "task `a`: is done but needs `c`, which is todo"},
		{"blocked task needs dropped", func(g *Graph) { g.Tasks[3].Needs = []string{"e"} }, "task `d`: needs `e`, which is dropped"},
		{"in-progress task needs dropped", func(g *Graph) { g.Tasks[1].Needs = []string{"e"} }, "task `b`: needs `e`, which is dropped"},
		{"done needs blocked", func(g *Graph) { g.Tasks[0].Needs = []string{"d"} }, "task `a`: is done but needs `d`, which is blocked"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := graphOf(task("a", Done), task("b", InProgress, "a"), task("c", Todo, "a", "b"), task("d", Blocked), task("e", Dropped))
			c.mutate(g)
			problems := g.Validate()
			for _, p := range problems {
				if strings.Contains(p, c.want) {
					return
				}
			}
			t.Fatalf("want a problem containing %q, got %q", c.want, problems)
		})
	}
}

func TestValidateCyclesReportedOnceWithPath(t *testing.T) {
	g := graphOf(task("a", Todo, "c"), task("b", Todo, "a"), task("c", Todo, "b"),
		task("x", Todo, "y"), task("y", Todo, "x"), task("z", Todo, "a"))
	var cycles []string
	for _, p := range g.Validate() {
		if strings.HasPrefix(p, "cycle: ") {
			cycles = append(cycles, p)
		}
	}
	want := []string{"cycle: a → c → b → a", "cycle: x → y → x"}
	if strings.Join(cycles, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", cycles, want)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	g := graphOf(task("a", Todo, "missing"), task("b", "nope"))
	g.Tasks[0].Title = ""
	if p := g.Validate(); len(p) != 3 {
		t.Fatalf("want 3 problems, got %q", p)
	}
}

func TestFrontier(t *testing.T) {
	cases := []struct {
		name string
		g    *Graph
		want string
	}{
		{"empty", graphOf(), ""},
		{"chain", graphOf(task("a", Done), task("b", Todo, "a"), task("c", Todo, "b")), "b"},
		{"diamond waits for both", graphOf(task("a", Done), task("b", Done, "a"), task("c", Todo, "a"), task("d", Todo, "b", "c")), "c"},
		{"diamond opens", graphOf(task("a", Done), task("b", Done, "a"), task("c", Done, "a"), task("d", Todo, "b", "c")), "d"},
		{"roots in file order", graphOf(task("z", Todo), task("a", Todo), task("m", Done)), "z,a"},
		{"in-progress and blocked aren't ready", graphOf(task("a", InProgress), task("b", Blocked), task("c", Todo)), "c"},
		{"blocked need holds dependents", graphOf(task("a", Blocked), task("b", Todo, "a")), ""},
		{"dropped tasks never ready", graphOf(task("a", Dropped), task("b", Dropped, "a")), ""},
		{"done not ready", graphOf(task("a", Done)), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ids(c.g.Ready(now)); got != c.want {
				t.Fatalf("ready = %q want %q", got, c.want)
			}
		})
	}
}

func TestLeaseLapseReleases(t *testing.T) {
	g := graphOf(task("a", InProgress), task("b", Todo, "a"))
	expires := g.Tasks[0].State.Claim.Expires
	if got := g.Tasks[0].Effective(expires.Add(-time.Nanosecond)); got != InProgress {
		t.Fatalf("before expiry: %s", got)
	}
	if got := g.Tasks[0].Effective(expires); got != Todo {
		t.Fatalf("at expiry: %s", got)
	}
	if got := ids(g.Ready(now)); got != "" {
		t.Fatalf("live lease: ready %q", got)
	}
	if got := ids(g.Ready(expires.Add(time.Minute))); got != "a" {
		t.Fatalf("lapsed lease: ready %q", got)
	}
	if got := (Task{State: State{Status: Done}}).Effective(now); got != Done {
		t.Fatalf("done: %s", got)
	}
}

func TestFrontierByID(t *testing.T) {
	g := graphOf(task("a", Todo), task("b", Done))
	if got, ok := g.ByID("b"); !ok || got.State.Status != Done {
		t.Fatalf("ByID(b) = %+v %v", got, ok)
	}
	if _, ok := g.ByID("zz"); ok {
		t.Fatal("ByID found a missing task")
	}
}

func TestValidateStoreRoundTrip(t *testing.T) {
	g := graphOf(task("a", Done), task("b", InProgress, "a"), task("c", Dropped))
	raw, err := store.Encode(File, g, Version)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Count(s, `"claim"`) != 1 || strings.Count(s, `"reason"`) != 1 {
		t.Fatalf("zero claims or empty reasons were encoded:\n%s", s)
	}
	var back Graph
	if err := store.Decode(File, raw, &back, Version); err != nil {
		t.Fatal(err)
	}
	if !back.Tasks[1].State.Claim.Expires.Equal(g.Tasks[1].State.Claim.Expires) || back.Tasks[0].State.Claim != (Claim{}) {
		t.Fatalf("claims didn't survive: %+v", back.Tasks)
	}
	if p := back.Validate(); len(p) != 0 {
		t.Fatalf("round trip invalid: %q", p)
	}
	bad := strings.Replace(s, `"title"`, `"titel"`, 1)
	if err := store.Decode(File, []byte(bad), &back, Version); err == nil || !strings.Contains(err.Error(), "did you mean `title`") {
		t.Fatalf("misspelled key: %v", err)
	}
}
