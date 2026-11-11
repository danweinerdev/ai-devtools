package graph

import (
	"strings"
	"testing"
	"time"

	"github.com/danweinerdev/ai-devtools/internal/store"
)

// The session is tool-owned and informational: claim-task --session records
// it, a later one replaces it, and every status change keeps it.

func TestSessionSetByClaim(t *testing.T) {
	g := claimsGraph()
	got, err := g.Claim("a", "oracle", "ses_build", time.Hour, t0)
	if err != nil || got.Session != "ses_build" {
		t.Fatalf("claim with a session: %+v %v", got, err)
	}
	valid(t, g)
}

func TestSessionReplacedByALaterClaim(t *testing.T) {
	g := claimsGraph()
	if _, err := g.Claim("a", "oracle", "ses_first", time.Hour, t0); err != nil {
		t.Fatal(err)
	}
	got, err := g.Claim("a", "oracle", "ses_second", time.Hour, t0.Add(time.Minute))
	if err != nil || got.Session != "ses_second" {
		t.Fatalf("renewal with another session: %+v %v", got, err)
	}
}

func TestSessionKeptByAClaimWithoutOne(t *testing.T) {
	g := claimsGraph()
	if _, err := g.Claim("a", "oracle", "ses_build", time.Hour, t0); err != nil {
		t.Fatal(err)
	}
	got, err := g.Claim("a", "oracle", "", time.Hour, t0.Add(time.Minute))
	if err != nil || got.Session != "ses_build" {
		t.Fatalf("renewal without a session: %+v %v", got, err)
	}
}

func TestSessionKeptByReleaseAndBlock(t *testing.T) {
	g := claimsGraph()
	if _, err := g.Claim("a", "oracle", "ses_build", time.Hour, t0); err != nil {
		t.Fatal(err)
	}
	got, err := g.Release("a", "oracle", t0)
	if err != nil || got.Session != "ses_build" {
		t.Fatalf("release: %+v %v", got, err)
	}
	got, err = g.Block("a", "waiting on a decision", "oracle", t0)
	if err != nil || got.Session != "ses_build" {
		t.Fatalf("block: %+v %v", got, err)
	}
	if g.Tasks[0].Session != "ses_build" {
		t.Fatalf("session stored as %q", g.Tasks[0].Session)
	}
	valid(t, g)
}

func TestSessionKeptByDrop(t *testing.T) {
	g := graphOf(task("a", Todo))
	if _, err := g.Claim("a", "oracle", "ses_build", time.Hour, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Release("a", "oracle", t0); err != nil {
		t.Fatal(err)
	}
	res, err := g.Drop("a", "superseded", nil, t0)
	if err != nil || res.Task.Session != "ses_build" {
		t.Fatalf("drop: %+v %v", res.Task, err)
	}
}

func TestSplitPartsDoNotInheritTheSession(t *testing.T) {
	g := graphOf(task("big", Todo), task("p", Todo))
	if _, err := g.Claim("big", "oracle", "ses_build", time.Hour, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Split("big", []string{"p"}, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	by := func(id string) Task { tk, _ := g.ByID(id); return tk }
	if got := by("big").Session; got != "ses_build" {
		t.Fatalf("split task kept %q, want ses_build", got)
	}
	if got := by("p").Session; got != "" {
		t.Fatalf("part inherited %q", got)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	g := graphOf(task("a", Todo), task("b", InProgress))
	g.Tasks[0].Session = "ses_done"
	raw, err := store.Encode(File, g, Version)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"session"`); n != 1 {
		t.Fatalf("encoded %d sessions, want 1:\n%s", n, raw)
	}
	var back Graph
	if err := store.Decode(File, raw, &back, Version); err != nil {
		t.Fatal(err)
	}
	if back.Tasks[0].Session != "ses_done" || back.Tasks[1].Session != "" {
		t.Fatalf("sessions didn't survive: %+v", back.Tasks)
	}
	if p := back.Validate(); len(p) != 0 {
		t.Fatalf("round trip invalid: %q", p)
	}

	// A file written before the field existed still loads.
	var old Graph
	if err := store.Decode(File, []byte(`{"version": 1, "tasks": [
  {"id": "a", "title": "t", "needs": [], "footprint": ["x"], "done": {"result": "r", "checks": ["true"]}, "state": {"status": "todo"}}
]}`), &old, Version); err != nil {
		t.Fatalf("a graph without a session didn't load: %v", err)
	}
	if old.Tasks[0].Session != "" {
		t.Fatalf("a graph without a session loaded one: %q", old.Tasks[0].Session)
	}
}
