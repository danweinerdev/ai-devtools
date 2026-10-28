package graph

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func claimsGraph() *Graph {
	return graphOf(task("a", Todo), task("b", Todo, "a"), task("c", Done))
}

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want error containing %q, got %v", want, err)
	}
}

func valid(t *testing.T, g *Graph) {
	t.Helper()
	if p := g.Validate(); len(p) > 0 {
		t.Fatalf("graph invalid after the change: %q", p)
	}
}

func TestClaimsDoubleClaimRefused(t *testing.T) {
	g := claimsGraph()
	got, err := g.Claim("a", "oracle", time.Hour, t0)
	if err != nil || got.State.Status != InProgress || got.State.Claim.Holder != "oracle" || !got.State.Claim.Expires.Equal(t0.Add(time.Hour)) {
		t.Fatalf("claim: %+v %v", got, err)
	}
	valid(t, g)
	_, err = g.Claim("a", "mechanic", time.Hour, t0.Add(time.Minute))
	wantErr(t, err, `task "a" is held by "oracle" until 2026-10-27T13:00:00Z`)
}

func TestClaimsRenewalExtendsExpiry(t *testing.T) {
	g := claimsGraph()
	g.Claim("a", "oracle", time.Hour, t0)
	got, err := g.Claim("a", "oracle", time.Hour, t0.Add(30*time.Minute))
	if err != nil || !got.State.Claim.Expires.Equal(t0.Add(90*time.Minute)) {
		t.Fatalf("renewal: %+v %v", got.State, err)
	}
}

func TestClaimsLapseThenAnotherHolderClaims(t *testing.T) {
	g := claimsGraph()
	g.Claim("a", "oracle", time.Hour, t0)
	got, err := g.Claim("a", "mechanic", time.Hour, t0.Add(time.Hour))
	if err != nil || got.State.Claim.Holder != "mechanic" {
		t.Fatalf("claim after lapse: %+v %v", got.State, err)
	}
}

func TestClaimsRefusals(t *testing.T) {
	g := claimsGraph()
	_, err := g.Claim("b", "h", time.Hour, t0)
	wantErr(t, err, `task "b" isn't ready; it needs a (todo)`)
	_, err = g.Claim("c", "h", time.Hour, t0)
	wantErr(t, err, `task "c" is done; only todo tasks can be claimed`)
	_, err = g.Claim("a", "", time.Hour, t0)
	wantErr(t, err, "needs a holder")
	_, err = g.Claim("a", "h", 0, t0)
	wantErr(t, err, "lease must be positive")
	_, err = g.Claim("zz", "h", time.Hour, t0)
	wantErr(t, err, `no task "zz" (3 tasks`)
}

func TestClaimsReleaseByHolderOnly(t *testing.T) {
	g := claimsGraph()
	g.Claim("a", "oracle", time.Hour, t0)
	_, err := g.Release("a", "mechanic", t0)
	wantErr(t, err, `held by "oracle"`)
	got, err := g.Release("a", "oracle", t0)
	if err != nil || got.State.Status != Todo || !got.State.Claim.IsZero() {
		t.Fatalf("release: %+v %v", got.State, err)
	}
	valid(t, g)
}

func TestClaimsReleaseLapsedClearsStaleClaim(t *testing.T) {
	g := claimsGraph()
	g.Claim("a", "oracle", time.Hour, t0)
	got, err := g.Release("a", "anyone", t0.Add(2*time.Hour))
	if err != nil || got.State.Status != Todo || !got.State.Claim.IsZero() {
		t.Fatalf("release of a lapsed claim: %+v %v", got.State, err)
	}
	_, err = g.Release("c", "h", t0)
	wantErr(t, err, `task "c" is done; only in-progress or blocked tasks can be released`)
	_, err = g.Release("b", "h", t0)
	wantErr(t, err, `task "b" is todo; only in-progress or blocked tasks can be released`)
}

func TestClaimsBlockAndUnblock(t *testing.T) {
	g := claimsGraph()
	_, err := g.Block("a", "  ", "", t0)
	wantErr(t, err, "needs a reason")
	g.Claim("a", "oracle", time.Hour, t0)
	_, err = g.Block("a", "waiting on vendor", "mechanic", t0)
	wantErr(t, err, `held by "oracle"`)
	got, err := g.Block("a", "waiting on vendor", "oracle", t0)
	if err != nil || got.State.Status != Blocked || got.State.Reason != "waiting on vendor" || !got.State.Claim.IsZero() {
		t.Fatalf("block: %+v %v", got.State, err)
	}
	valid(t, g)
	_, err = g.Block("a", "again", "", t0)
	wantErr(t, err, `task "a" is blocked; only todo or in-progress tasks can be blocked`)
	_, err = g.Claim("a", "oracle", time.Hour, t0)
	wantErr(t, err, "is blocked")

	got, err = g.Release("a", "", t0)
	if err != nil || got.State.Status != Todo || got.State.Reason != "" {
		t.Fatalf("unblock via release: %+v %v", got.State, err)
	}
	valid(t, g)
	// Anyone may block an unclaimed task, and a lapsed claim no longer protects.
	if _, err := g.Block("a", "r", "", t0); err != nil {
		t.Fatal(err)
	}
	g.Release("a", "", t0)
	g.Claim("a", "oracle", time.Hour, t0)
	if _, err := g.Block("a", "r", "", t0.Add(time.Hour)); err != nil {
		t.Fatalf("block after the lease lapsed: %v", err)
	}
}

func TestClaimsHeldBy(t *testing.T) {
	g := claimsGraph()
	_, err := g.HeldBy("a", "oracle", t0)
	wantErr(t, err, `task "a" is todo; claim it first`)
	g.Claim("a", "oracle", time.Hour, t0)
	_, err = g.HeldBy("a", "mechanic", t0)
	wantErr(t, err, `held by "oracle", not "mechanic"`)
	if _, err := g.HeldBy("a", "oracle", t0.Add(3*time.Hour)); err != nil {
		t.Fatalf("a lapsed lease still counts for its holder: %v", err)
	}
}
