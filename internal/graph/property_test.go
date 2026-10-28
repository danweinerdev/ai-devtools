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

// G7: random valid graphs, random operation sequences. After every success
// the graph validates; after every refusal it's deep-equal to before; nothing
// panics. A failure names its seed and step so it reproduces exactly.

const (
	propertySeeds = 200
	propertySteps = 50
)

// randomGraph builds a valid DAG: needs point only at earlier tasks, and each
// status is drawn from those its needs allow.
func randomGraph(r *rand.Rand, at time.Time) *Graph {
	n := 1 + r.Intn(25)
	var tasks []Task
	status := map[string]string{}
	for i := range n {
		id := fmt.Sprintf("t%d", i)
		var needs []string
		for j := range i {
			if r.Intn(4) == 0 {
				needs = append(needs, fmt.Sprintf("t%d", j))
			}
		}
		allDone, noneDropped, doneOrDropped := true, true, true
		for _, x := range needs {
			allDone = allDone && status[x] == Done
			noneDropped = noneDropped && status[x] != Dropped
			doneOrDropped = doneOrDropped && (status[x] == Done || status[x] == Dropped)
		}
		allowed := []string{Dropped}
		if noneDropped {
			allowed = append(allowed, Todo, Blocked)
		}
		if allDone {
			allowed = append(allowed, InProgress)
		}
		if doneOrDropped {
			allowed = append(allowed, Done)
		}
		st := allowed[r.Intn(len(allowed))]
		x := task(id, st, needs...)
		if st == InProgress {
			x.State.Claim = Claim{Holder: holders[r.Intn(len(holders))],
				Expires: at.Add(time.Duration(r.Intn(120)-30) * time.Minute)}
		}
		status[id] = st
		tasks = append(tasks, x)
	}
	return graphOf(tasks...)
}

var holders = []string{"h1", "h2"}

// complete mirrors complete-task: an in-progress task held by holder becomes done.
// Its checks are assumed to pass; needs are already done (Validate guarantees
// that for an in-progress task).
func complete(g *Graph, id, holder string, at time.Time) error {
	t, err := g.HeldBy(id, holder, at)
	if err != nil {
		return err
	}
	i, _ := g.find(t.ID)
	g.Tasks[i].State = State{Status: Done}
	return nil
}

func pick[T any](r *rand.Rand, xs []T) T { return xs[r.Intn(len(xs))] }

func someIDs(r *rand.Rand, g *Graph, max int) []string {
	var out []string
	for range r.Intn(max + 1) {
		out = append(out, pick(r, g.Tasks).ID)
	}
	return out
}

func TestPropertyRandomOperations(t *testing.T) {
	for seed := int64(1); seed <= propertySeeds; seed++ {
		r := rand.New(rand.NewSource(seed))
		at := now
		g := randomGraph(r, at)
		if p := g.Validate(); len(p) > 0 {
			t.Fatalf("seed %d: generator made an invalid graph: %q", seed, p)
		}
		for step := range propertySteps {
			at = at.Add(time.Duration(r.Intn(40)) * time.Minute)
			id := pick(r, g.Tasks).ID
			holder := pick(r, holders)
			var name string
			var op func() error
			switch r.Intn(8) {
			case 0:
				name = "claim " + id
				op = func() error { _, err := g.Claim(id, holder, time.Duration(1+r.Intn(60))*time.Minute, at); return err }
			case 1:
				name = "release " + id
				op = func() error { _, err := g.Release(id, holder, at); return err }
			case 2:
				name = "block " + id
				op = func() error { _, err := g.Block(id, "stuck", holder, at); return err }
			case 3:
				needs := someIDs(r, g, 3)
				name = fmt.Sprintf("update %s needs %v", id, needs)
				op = func() error { _, err := g.UpdateTask(id, Edit{Needs: &needs}); return err }
			case 4:
				parts := someIDs(r, g, 3)
				name = fmt.Sprintf("split %s into %v", id, parts)
				op = func() error { _, err := g.Split(id, parts, at); return err }
			case 5:
				repl := someIDs(r, g, 2)
				name = fmt.Sprintf("drop %s replaced by %v", id, repl)
				op = func() error { _, err := g.Drop(id, "gone", repl, at); return err }
			case 6:
				name = "cascade " + id
				op = func() error { _, err := g.DropCascade(id, "gone", at); return err }
			default:
				name = "complete " + id
				op = func() error { return complete(g, id, holder, at) }
			}

			before := cloneTasks(g.Tasks)
			err := func() (err error) {
				defer func() {
					if p := recover(); p != nil {
						err = fmt.Errorf("panic: %v", p)
					}
				}()
				return op()
			}()
			where := fmt.Sprintf("seed %d step %d (%s)", seed, step, name)
			switch {
			case err != nil && strings.HasPrefix(err.Error(), "panic: "):
				t.Fatalf("%s: %v", where, err)
			case err != nil && !reflect.DeepEqual(g.Tasks, before):
				t.Fatalf("%s: refused (%v) but changed the graph", where, err)
			case err == nil:
				if p := g.Validate(); len(p) > 0 {
					t.Fatalf("%s: succeeded but left the graph invalid: %q", where, p)
				}
			}
		}
	}
}

// The generator must actually exercise every operation's success path, or
// the property test proves little.
func TestPropertyOperationsSucceedSometimes(t *testing.T) {
	succeeded := map[string]int{}
	for seed := int64(1); seed <= propertySeeds; seed++ {
		r := rand.New(rand.NewSource(seed))
		g := randomGraph(r, now)
		for _, x := range slices.Clone(g.Tasks) {
			c := cloneTasks(g.Tasks)
			try := func(name string, err error) {
				if err == nil {
					succeeded[name]++
				}
				g.Tasks = cloneTasks(c)
			}
			_, err := g.Claim(x.ID, "h1", time.Hour, now)
			try("claim", err)
			_, err = g.Release(x.ID, x.State.Claim.Holder, now)
			try("release", err)
			_, err = g.Block(x.ID, "stuck", x.State.Claim.Holder, now)
			try("block", err)
			_, err = g.UpdateTask(x.ID, Edit{Title: strp("renamed")})
			try("update", err)
			_, err = g.Drop(x.ID, "gone", nil, now)
			try("drop", err)
			_, err = g.DropCascade(x.ID, "gone", now)
			try("cascade", err)
			try("complete", complete(g, x.ID, x.State.Claim.Holder, now))
			for _, p := range g.Tasks {
				_, err = g.Split(x.ID, []string{p.ID}, now)
				try("split", err)
			}
		}
	}
	for _, name := range []string{"claim", "release", "block", "update", "drop", "cascade", "complete", "split"} {
		if succeeded[name] == 0 {
			t.Errorf("%s never succeeded on the generated graphs", name)
		}
	}
}
