package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The race tests run two (or more) real aid processes against one graph at
// the same moment: an edit that rewires or retires tasks against a claim on
// the same tasks. Whichever wins, the loser must be refused on a fresh read
// under the lock, and the graph must end up valid with every invariant held.
// They assert outcome-consistency for every interleaving, not a winner; each
// race runs on many fresh repos with the start order shuffled so both orders
// happen.

const raceRounds = 30

// raceTask is a task as `show --json` prints it.
type raceTask struct {
	ID    string   `json:"id"`
	Needs []string `json:"needs"`
	State struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
		Claim  struct {
			Holder string `json:"holder"`
		} `json:"claim"`
	} `json:"state"`
}

// graphOf reads the whole graph through `show --json`.
func graphOf(t *testing.T, dir string) map[string]raceTask {
	t.Helper()
	c := runAid(dir, "show", "--json")
	if c.code != 0 {
		t.Fatalf("show: %s", c)
	}
	var shown struct {
		Tasks []raceTask `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(c.out), &shown); err != nil {
		t.Fatalf("show --json: %v\n%s", err, c.out)
	}
	g := map[string]raceTask{}
	for _, task := range shown.Tasks {
		g[task.ID] = task
	}
	return g
}

// invariantProblems checks what must hold after any interleaving: `validate`
// passes, a done task's needs are done or dropped, and an in-progress task's needs
// are all done (it was claimed when they were, and nothing may take that
// away while it's held).
func invariantProblems(t *testing.T, dir string) []string {
	t.Helper()
	var problems []string
	if c := runAid(dir, "validate"); c.code != 0 {
		problems = append(problems, "validate failed: "+c.String())
	}
	g := graphOf(t, dir)
	for _, task := range g {
		for _, n := range task.Needs {
			dep := g[n].State.Status
			switch task.State.Status {
			case "done":
				if dep != "done" && dep != "dropped" {
					problems = append(problems, fmt.Sprintf("done task %s needs %s, which is %s", task.ID, n, dep))
				}
			case "in-progress":
				if dep != "done" {
					problems = append(problems, fmt.Sprintf("in-progress task %s needs %s, which is %s", task.ID, n, dep))
				}
			}
		}
	}
	return problems
}

func assertInvariants(t *testing.T, dir string) {
	t.Helper()
	for _, p := range invariantProblems(t, dir) {
		t.Error(p)
	}
}

// race runs every command at once, in shuffled start order, and returns
// their outcomes in the order given.
func race(dir string, rng *rand.Rand, cmds ...[]string) []call {
	results := make([]call, len(cmds))
	order := rng.Perm(len(cmds))
	var start, done sync.WaitGroup
	start.Add(1)
	for _, i := range order {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			results[i] = runAid(dir, cmds[i]...)
		}(i)
	}
	start.Done()
	done.Wait()
	return results
}

// mustRefuse fails unless c is a plain refusal (exit 1) whose message
// contains one of wants.
func mustRefuse(t *testing.T, c call, wants ...string) {
	t.Helper()
	if c.code != 1 {
		t.Fatalf("want a refusal, got %s", c)
	}
	for _, w := range wants {
		if strings.Contains(c.errOut, w) {
			return
		}
	}
	t.Fatalf("refusal doesn't say any of %q: %s", wants, c)
}

// raceRepo is a fresh initialised repo whose tasks are added by setup.
func raceRepo(t *testing.T, setup func(r *repo)) *repo {
	t.Helper()
	r := newRepo(t)
	r.ok("init", "Feature")
	setup(r)
	return r
}

// TestRaceSplitVsClaim: split-task big racing claim-task big. The split is
// refused if the claim landed first (live claim); the claim is refused if the
// split landed first (big is dropped). Exactly one wins.
func TestRaceSplitVsClaim(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	wins := map[string]int{}
	for round := 0; round < raceRounds; round++ {
		r := raceRepo(t, func(r *repo) {
			r.addTask("big")
			r.addTask("p1")
			r.addTask("p2")
			r.addTask("dep", "big")
		})
		res := race(r.dir, rng,
			[]string{"split-task", "big", "--into", "p1,p2"},
			[]string{"claim-task", "big", "--holder", "w"})
		split, claim := res[0], res[1]
		g := graphOf(t, r.dir)
		switch {
		case claim.code == 0 && split.code != 0:
			wins["claim"]++
			mustRefuse(t, split, "held by", "live claim", "claimed")
			if g["big"].State.Status != "in-progress" || g["big"].State.Claim.Holder != "w" {
				t.Fatalf("claim won but big is %+v", g["big"])
			}
			if len(g["p1"].Needs) != 0 || len(g["p2"].Needs) != 0 || strings.Join(g["dep"].Needs, ",") != "big" {
				t.Fatalf("claim won but the split changed needs: %+v", g)
			}
		case split.code == 0 && claim.code != 0:
			wins["split"]++
			mustRefuse(t, claim, "only todo tasks can be claimed")
			if g["big"].State.Status != "dropped" || !strings.HasPrefix(g["big"].State.Reason, "split into") {
				t.Fatalf("split won but big is %+v", g["big"])
			}
			if strings.Join(g["dep"].Needs, ",") != "p1,p2" {
				t.Fatalf("split won but dep needs %v", g["dep"].Needs)
			}
		default:
			t.Fatalf("want exactly one winner:\n%s\n%s", split, claim)
		}
		assertInvariants(t, r.dir)
	}
	t.Logf("split vs claim over %d rounds: %v", raceRounds, wins)
}

// TestRaceDropVsClaim: drop-task a racing claim-task a. Exactly one wins.
func TestRaceDropVsClaim(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	wins := map[string]int{}
	for round := 0; round < raceRounds; round++ {
		r := raceRepo(t, func(r *repo) { r.addTask("a") })
		res := race(r.dir, rng,
			[]string{"drop-task", "a", "--reason", "x"},
			[]string{"claim-task", "a", "--holder", "w"})
		drop, claim := res[0], res[1]
		a := graphOf(t, r.dir)["a"]
		switch {
		case claim.code == 0 && drop.code != 0:
			wins["claim"]++
			mustRefuse(t, drop, "held by", "live claim", "claimed")
			if a.State.Status != "in-progress" {
				t.Fatalf("claim won but a is %+v", a)
			}
		case drop.code == 0 && claim.code != 0:
			wins["drop"]++
			mustRefuse(t, claim, "only todo tasks can be claimed")
			if a.State.Status != "dropped" || a.State.Reason != "x" {
				t.Fatalf("drop won but a is %+v", a)
			}
		default:
			t.Fatalf("want exactly one winner:\n%s\n%s", drop, claim)
		}
		assertInvariants(t, r.dir)
	}
	t.Logf("drop vs claim over %d rounds: %v", raceRounds, wins)
}

// TestRaceDropReplacedByVsClaimDependent: a is done, b needs a, r is todo.
// drop-task a --replaced-by r racing claim-task b. If the drop lands first, b
// needs r (todo) and the claim is refused as not ready. If the claim lands
// first, b is in-progress on a done need, and rewiring it onto a todo task would
// leave a held task with an unmet need: the drop must be refused. Both may
// succeed only if the invariants still hold.
func TestRaceDropReplacedByVsClaimDependent(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	wins := map[string]int{}
	for round := 0; round < raceRounds; round++ {
		r := raceRepo(t, func(r *repo) {
			r.addTask("a")
			r.finishTask("a")
			r.addTask("b", "a")
			r.addTask("r")
		})
		res := race(r.dir, rng,
			[]string{"drop-task", "a", "--reason", "replaced", "--replaced-by", "r"},
			[]string{"claim-task", "b", "--holder", "w"})
		drop, claim := res[0], res[1]
		switch {
		case drop.code == 0 && claim.code == 0:
			wins["both"]++
		case drop.code == 0:
			wins["drop"]++
			mustRefuse(t, claim, "isn't ready")
		case claim.code == 0:
			wins["claim"]++
			if drop.code != 1 {
				t.Fatalf("drop failed abnormally: %s", drop)
			}
		default:
			t.Fatalf("neither won:\n%s\n%s", drop, claim)
		}
		assertInvariants(t, r.dir)
		if t.Failed() {
			t.Fatalf("round %d (%v so far):\n%s\n%s", round, wins, drop, claim)
		}
	}
	t.Logf("drop --replaced-by vs claim of a dependent over %d rounds: %v", raceRounds, wins)
}

// TestRaceUpdateNeedsVsClaim: update-task b --needs r (r todo) racing
// claim-task b. If the update lands first, the claim is refused as not
// ready. If the claim lands first, giving the held task an unmet need must
// be refused. Both may succeed only if the invariants still hold.
func TestRaceUpdateNeedsVsClaim(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	wins := map[string]int{}
	for round := 0; round < raceRounds; round++ {
		r := raceRepo(t, func(r *repo) {
			r.addTask("b")
			r.addTask("r")
		})
		res := race(r.dir, rng,
			[]string{"update-task", "b", "--needs", "r"},
			[]string{"claim-task", "b", "--holder", "w"})
		update, claim := res[0], res[1]
		switch {
		case update.code == 0 && claim.code == 0:
			wins["both"]++
		case update.code == 0:
			wins["update"]++
			mustRefuse(t, claim, "isn't ready")
		case claim.code == 0:
			wins["claim"]++
			if update.code != 1 {
				t.Fatalf("update failed abnormally: %s", update)
			}
		default:
			t.Fatalf("neither won:\n%s\n%s", update, claim)
		}
		assertInvariants(t, r.dir)
		if t.Failed() {
			t.Fatalf("round %d (%v so far):\n%s\n%s", round, wins, update, claim)
		}
	}
	t.Logf("update --needs vs claim over %d rounds: %v", raceRounds, wins)
}

// unexpected reports whether a failed call is something other than an
// ordinary refusal: a crash, a lock timeout, a non-cooperating-writer error,
// an internal error, or a malformed command line (a test bug).
func unexpected(c call) bool {
	if c.code == 0 {
		return false
	}
	if c.code != 1 {
		return true
	}
	for _, bad := range []string{"timed out", "aid bug", "changed by something", "panic", "goroutine ",
		"unknown flag", "required flag", "accepts ", "unknown command"} {
		if strings.Contains(c.errOut, bad) {
			return true
		}
	}
	return false
}

// TestRaceManyWay: six processes loop random graph operations — claim,
// complete, release, block, update needs, split, drop — on a 40-task graph
// with chains, for about two seconds. Refusals are expected; nothing else is.
// Afterwards the graph is valid, every invariant holds, and nothing was left
// behind.
func TestRaceManyWay(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	const n = 40
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("t%02d", i)
		// Chains of four: every task but the first of each chain needs the
		// previous one.
		if i%4 == 0 {
			r.addTask(ids[i])
		} else {
			r.addTask(ids[i], ids[i-1])
		}
	}

	const workers = 6
	deadline := time.Now().Add(2 * time.Second)
	var fails failures
	var wg sync.WaitGroup
	counts := make([]map[string]int, workers)
	for w := 0; w < workers; w++ {
		counts[w] = map[string]int{}
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(100 + w)))
			holder := fmt.Sprintf("w%d", w)
			pick := func() string { return ids[rng.Intn(n)] }
			for time.Now().Before(deadline) {
				id := pick()
				var args []string
				var op string
				switch rng.Intn(7) {
				case 0, 1:
					op, args = "claim", []string{"claim-task", id, "--holder", holder}
				case 2:
					op, args = "complete", []string{"complete-task", id, "--holder", holder}
				case 3:
					op, args = "release", []string{"release-task", id, "--holder", holder}
				case 4:
					op, args = "block", []string{"block-task", id, "--reason", "stuck", "--holder", holder}
				case 5:
					needs := []string{}
					for k := rng.Intn(3); k > 0; k-- {
						needs = append(needs, pick())
					}
					op, args = "update", []string{"update-task", id, "--needs=" + strings.Join(needs, ",")}
				default:
					if rng.Intn(2) == 0 {
						op, args = "split", []string{"split-task", id, "--into", pick() + "," + pick()}
					} else {
						op, args = "drop", []string{"drop-task", id, "--reason", "race"}
						if rng.Intn(2) == 0 {
							args = append(args, "--replaced-by", pick())
						}
					}
				}
				c := runAid(r.dir, args...)
				if unexpected(c) {
					fails.add("%s", c)
					continue
				}
				if c.code == 0 {
					counts[w][op+" ok"]++
				} else {
					counts[w][op+" refused"]++
				}
			}
		}(w)
	}
	wg.Wait()
	for _, f := range fails.list {
		t.Error(f)
	}

	total := map[string]int{}
	for _, c := range counts {
		for k, v := range c {
			total[k] += v
		}
	}
	t.Logf("many-way over 2s: %v", total)
	if total["claim ok"] == 0 || total["complete ok"]+total["drop ok"]+total["split ok"] == 0 {
		t.Errorf("the race exercised too little: %v", total)
	}

	assertInvariants(t, r.dir)
	err := filepath.WalkDir(filepath.Join(r.dir, ".aid"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(d.Name(), ".aid-tmp-") || strings.HasPrefix(d.Name(), ".init-") {
			t.Errorf("leftover %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
