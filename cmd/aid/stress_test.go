package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The stress test drives the real binary from many concurrent processes:
// workers race to claim and complete tasks while others add tasks, add and
// resolve items, and read. It proves the lock and the compare-and-swap hold
// under load: no lost or doubled write, no corrupt file, no lock timeout.

// call is one aid invocation's outcome. Safe to use from any goroutine.
type call struct {
	args        []string
	out, errOut string
	code        int
}

func (c call) String() string {
	return fmt.Sprintf("aid %s: exit %d\n%s%s", strings.Join(c.args, " "), c.code, c.out, c.errOut)
}

func runAid(dir string, args ...string) call {
	cmd := exec.Command(aidBin, args...)
	cmd.Dir = dir
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	c := call{args: args, out: out.String(), errOut: errOut.String()}
	if ee, ok := err.(*exec.ExitError); ok {
		c.code = ee.ExitCode()
	} else if err != nil {
		c.code, c.errOut = -1, err.Error()
	}
	return c
}

// failures collects unexpected outcomes from every goroutine.
type failures struct {
	mu   sync.Mutex
	list []string
}

func (f *failures) add(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.list) < 20 {
		f.list = append(f.list, fmt.Sprintf(format, args...))
	}
}

// expectedClaimRefusal is a lost race: someone else holds the task, or
// already finished it.
func expectedClaimRefusal(errOut string) bool {
	return strings.Contains(errOut, "is held by") || strings.Contains(errOut, "only todo tasks can be claimed")
}

func TestStressConcurrentProcesses(t *testing.T) {
	r := newRepo(t)
	dir := r.dir
	r.ok("init", "Feature")

	add := func(id string, needs ...string) {
		args := []string{"add-task", "--id", id, "--title", "task " + id, "--footprint", "src/" + id,
			"--result", "it works", "--check", "true"}
		if len(needs) > 0 {
			args = append(args, "--needs", strings.Join(needs, ","))
		}
		r.ok(args...)
	}
	initial := 0
	for i := 1; i <= 30; i++ {
		add(fmt.Sprintf("t%02d", i))
		initial++
	}
	for c := 1; c <= 3; c++ { // chains a → b → c
		prev := ""
		for _, step := range []string{"a", "b", "c"} {
			id := fmt.Sprintf("chain%d-%s", c, step)
			if prev == "" {
				add(id)
			} else {
				add(id, prev)
			}
			prev = id
			initial++
		}
	}

	const workers, addedTasks, addedItems = 8, 10, 20
	var (
		fail        failures
		completedMu sync.Mutex
		completed   = map[string]string{} // task ID -> holder that completed it
		itemIDs     = make(chan string, addedItems)
		addersDone  atomic.Bool
		stopReaders atomic.Bool
		wg, readers sync.WaitGroup
	)

	allDone := func() (bool, error) {
		c := runAid(dir, "show", "--json")
		if c.code != 0 {
			return false, fmt.Errorf("%s", c)
		}
		var g struct {
			Tasks []struct {
				State struct {
					Status string `json:"status"`
				} `json:"state"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal([]byte(c.out), &g); err != nil {
			return false, err
		}
		for _, task := range g.Tasks {
			if task.State.Status != "done" {
				return false, nil
			}
		}
		return true, nil
	}

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(holder string, rng *rand.Rand) {
			defer wg.Done()
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				c := runAid(dir, "ready", "--json")
				if c.code != 0 {
					fail.add("%s", c)
					return
				}
				var ready []struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal([]byte(c.out), &ready); err != nil {
					fail.add("ready --json: %v\n%s", err, c.out)
					return
				}
				if len(ready) == 0 {
					if addersDone.Load() {
						done, err := allDone()
						if err != nil {
							fail.add("%v", err)
							return
						}
						if done {
							return
						}
					}
					time.Sleep(10 * time.Millisecond)
					continue
				}
				id := ready[rng.Intn(len(ready))].ID
				claim := runAid(dir, "claim-task", id, "--holder", holder)
				if claim.code != 0 {
					if !expectedClaimRefusal(claim.errOut) {
						fail.add("unexpected claim refusal: %s", claim)
					}
					continue
				}
				done := runAid(dir, "complete-task", id, "--holder", holder)
				if done.code != 0 {
					fail.add("complete after a successful claim failed: %s", done)
					continue
				}
				completedMu.Lock()
				if prev, twice := completed[id]; twice {
					fail.add("task %s completed twice: by %s and %s", id, prev, holder)
				}
				completed[id] = holder
				completedMu.Unlock()
			}
			fail.add("worker %s ran out of time", holder)
		}(fmt.Sprintf("w%d", w), rand.New(rand.NewSource(int64(w))))
	}

	// Add tasks while the workers run, some needing initial tasks.
	var adders sync.WaitGroup
	adders.Add(2)
	go func() {
		defer adders.Done()
		for i := 1; i <= addedTasks; i++ {
			args := []string{"add-task", "--id", fmt.Sprintf("n%02d", i), "--title", "late", "--footprint", "late",
				"--result", "r", "--check", "true"}
			if i%2 == 0 {
				args = append(args, "--needs", fmt.Sprintf("t%02d", i))
			}
			if c := runAid(dir, args...); c.code != 0 {
				fail.add("%s", c)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	// Add items and resolve every other one as it goes.
	go func() {
		defer adders.Done()
		for i := 1; i <= addedItems; i++ {
			c := runAid(dir, "add-item", "--kind", "note", "--text", fmt.Sprintf("note %d", i), "--json")
			if c.code != 0 {
				fail.add("%s", c)
				continue
			}
			var item struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(c.out), &item); err != nil {
				fail.add("add-item --json: %v\n%s", err, c.out)
				continue
			}
			itemIDs <- item.ID
			if i%2 == 0 {
				if c := runAid(dir, "resolve-item", item.ID, "--as", "done", "--where", "stress"); c.code != 0 {
					fail.add("%s", c)
				}
			}
		}
	}()
	// Readers: show and validate throughout.
	readers.Add(1)
	go func() {
		defer readers.Done()
		for !stopReaders.Load() {
			for _, args := range [][]string{{"show"}, {"validate"}, {"items", "--json"}} {
				if c := runAid(dir, args...); c.code != 0 {
					fail.add("reader saw: %s", c)
				}
			}
		}
	}()

	adders.Wait()
	addersDone.Store(true)
	wg.Wait()
	stopReaders.Store(true)
	readers.Wait()
	close(itemIDs)

	for _, f := range fail.list {
		t.Error(f)
	}
	if t.Failed() {
		t.FailNow()
	}

	// Every task done, each completed exactly once.
	if want := initial + addedTasks; len(completed) != want {
		t.Fatalf("completed %d tasks, want %d", len(completed), want)
	}
	if done, err := allDone(); err != nil || !done {
		t.Fatalf("not every task is done (err %v)", err)
	}
	contains(t, r.ok("validate"), "valid")

	// Every added item exists exactly once, with unique IDs.
	var items []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(r.ok("items", "--json")), &items); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, it := range items {
		seen[it.ID]++
	}
	added := 0
	for id := range itemIDs {
		added++
		if seen[id] != 1 {
			t.Errorf("item %s appears %d times", id, seen[id])
		}
	}
	if added != addedItems || len(items) != addedItems || len(seen) != addedItems {
		t.Fatalf("items: added %d, stored %d, unique %d; want %d", added, len(items), len(seen), addedItems)
	}

	// No crash leftovers anywhere under .aid/.
	filepath.WalkDir(filepath.Join(dir, AidDir), func(path string, d os.DirEntry, err error) error {
		if err == nil && (strings.Contains(d.Name(), ".aid-tmp-") || strings.HasPrefix(d.Name(), ".init-")) {
			t.Errorf("leftover %s", path)
		}
		return nil
	})

	// finish refuses while items are open, then succeeds.
	contains(t, r.refused("finish"), "item(s) open")
	for _, it := range items {
		if it.Status == "open" {
			r.ok("resolve-item", it.ID, "--as", "done", "--where", "stress")
		}
	}
	contains(t, r.ok("finish"), fmt.Sprintf("finished: %d done, 0 dropped", initial+addedTasks))
}
