package graph

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Dependent is a task that needs the task being dropped directly: the first
// generation, whose needs are what must move.
type Dependent struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Blocks marks a dependent with work still ahead of it (todo, in-progress or
	// blocked); each must be decided before the drop can go ahead. A done
	// dependent never blocks and keeps its need as history.
	Blocks bool `json:"blocks"`
}

// DropRefusal is the answer when tasks with work ahead still need the task
// being dropped. It lists only the direct dependents, plus how many further
// tasks with work ahead hang below them: past one generation, reassigning or
// dropping the whole subtree beats deciding task by task.
type DropRefusal struct {
	ID         string      `json:"id"`
	Dependents []Dependent `json:"dependents"`
	// Transitive counts the tasks with work ahead that need the direct
	// dependents in turn, through other tasks with work ahead.
	Transitive int `json:"transitive"`
}

func (r *DropRefusal) Error() string {
	var b strings.Builder
	blocking := 0
	for _, d := range r.Dependents {
		if d.Blocks {
			blocking++
		}
	}
	fmt.Fprintf(&b, "can't drop %q: %d task(s) with work ahead need it directly\n", r.ID, blocking)
	for _, d := range r.Dependents {
		fmt.Fprintf(&b, "  %s (%s)", d.ID, d.Status)
		if d.Blocks {
			b.WriteString("  [blocks]")
		} else {
			b.WriteString("  [done: keeps its need as history]")
		}
		b.WriteString("\n")
	}
	if r.Transitive > 0 {
		fmt.Fprintf(&b, "%d more task(s) with work ahead need those in turn, so handle the whole subtree:\n"+
			"  - reassign: drop with --replaced-by <ids>, moving every task marked [blocks] onto the replacements\n"+
			"  - drop all: drop with --cascade, dropping %q and every task with work ahead below it", r.Transitive, r.ID)
		return b.String()
	}
	fmt.Fprintf(&b, "Decide each task marked [blocks], then drop again:\n"+
		"  - it still applies and %[1]q was replaced: update-task its --needs, or drop with --replaced-by <ids>\n"+
		"  - it still applies and the prerequisite went away: update-task to remove the need\n"+
		"  - it no longer applies: drop with --cascade, or drop it first", r.ID)
	return b.String()
}

// DropResult says what a drop did, so the orchestrator can report it.
type DropResult struct {
	Task Task `json:"task"`
	// Dropped lists every task the drop marked dropped: the task itself, then
	// with --cascade the rest of its subtree in file order.
	Dropped []string `json:"dropped"`
	// Rewired lists the dependents whose need moved onto the replacements.
	Rewired []string `json:"rewired"`
	// KeptAsHistory lists done tasks that still name a dropped task among
	// their needs, as a record of what they were built on.
	KeptAsHistory []string `json:"keptAsHistory"`
	// Dependents is the direct dependents as they stood before the drop.
	Dependents []Dependent `json:"dependents"`
}

// neededBy maps each ID to the positions of the tasks that aren't dropped and
// need it, in file order.
func (g *Graph) neededBy() map[string][]int {
	by := map[string][]int{}
	for j, t := range g.Tasks {
		if t.State.Status == Dropped {
			continue
		}
		for _, n := range t.Needs {
			by[n] = append(by[n], j)
		}
	}
	return by
}

// workAheadAt is true for a task with work still ahead of it at now.
func workAheadAt(t Task, now time.Time) bool {
	switch t.Effective(now) {
	case Todo, InProgress, Blocked:
		return true
	}
	return false
}

// Dependents lists the tasks that aren't dropped and need id directly, in
// file order. O(n+e).
func (g *Graph) Dependents(id string, now time.Time) []Dependent {
	out := []Dependent{}
	seen := map[int]bool{}
	for _, j := range g.neededBy()[id] {
		t := g.Tasks[j]
		if t.ID == id || seen[j] {
			continue
		}
		seen[j] = true
		out = append(out, Dependent{ID: t.ID, Status: t.Effective(now), Blocks: workAheadAt(t, now)})
	}
	return out
}

// subtree is the positions of every task with work ahead that needs id,
// directly or through other tasks with work ahead, in file order. A done
// task stops the walk: it keeps its need as history, and what needs it still
// needs a done task. O(n+e).
func (g *Graph) subtree(id string, now time.Time, by map[string][]int) []int {
	in := map[int]bool{}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, j := range by[cur] {
			t := g.Tasks[j]
			if in[j] || t.ID == id || !workAheadAt(t, now) {
				continue
			}
			in[j] = true
			queue = append(queue, t.ID)
		}
	}
	out := make([]int, 0, len(in))
	for j := range in {
		out = append(out, j)
	}
	sort.Ints(out)
	return out
}

// Drop marks id dropped with reason. A dropped task stays in the graph as a
// record, leaves the check batch, and stops counting toward finishing.
//
// It's refused while a claim on id is live, and while a direct dependent
// still has work ahead (todo, in-progress or blocked) — unless replacedBy is given,
// in which case each such dependent's need on id moves onto the replacements
// in the same change. Done dependents never block and keep their need as
// history. The graph is validated before Drop returns, so a rewiring that
// would create a cycle is refused with nothing changed for the caller to save.
func (g *Graph) Drop(id, reason string, replacedBy []string, now time.Time) (DropResult, error) {
	i, err := g.find(id)
	if err != nil {
		return DropResult{}, err
	}
	t := g.Tasks[i]
	switch {
	case strings.TrimSpace(reason) == "":
		return DropResult{}, fmt.Errorf("dropping %q needs a reason (--reason)", id)
	case t.State.Status == Dropped:
		return DropResult{}, fmt.Errorf("task %q is already dropped (%s)", id, t.State.Reason)
	case t.Effective(now) == InProgress:
		return DropResult{}, fmt.Errorf("task %q is claimed by %q until %s; it can't be dropped while the claim is live",
			id, t.State.Claim.Holder, t.State.Claim.Expires.Format(time.RFC3339))
	}

	var replacements []string
	for _, r := range replacedBy {
		rt, ok := g.ByID(r) // a handful of replacements: a scan each is fine
		switch {
		case r == id:
			return DropResult{}, fmt.Errorf("task %q can't replace itself", id)
		case !ok:
			return DropResult{}, fmt.Errorf("replacement %q doesn't exist; add it first", r)
		case rt.State.Status == Dropped:
			return DropResult{}, fmt.Errorf("replacement %q is dropped", r)
		}
		if !slices.Contains(replacements, r) {
			replacements = append(replacements, r)
		}
	}

	deps := g.Dependents(id, now)
	res := DropResult{Dropped: []string{id}, Rewired: []string{}, KeptAsHistory: []string{}, Dependents: deps}
	for _, d := range deps {
		if !d.Blocks {
			res.KeptAsHistory = append(res.KeptAsHistory, d.ID)
			continue
		}
		if len(replacements) == 0 {
			return DropResult{}, g.refusal(id, deps, now)
		}
		res.Rewired = append(res.Rewired, d.ID)
	}

	for _, depID := range res.Rewired {
		if slices.Contains(replacements, depID) {
			return DropResult{}, fmt.Errorf("replacement %q needs %q itself; it can't also replace it", depID, id)
		}
	}

	// Every refusal above leaves g untouched. The change itself is made on a
	// copy, adopted only once the result validates.
	next := &Graph{Version: g.Version, Tasks: cloneTasks(g.Tasks)}
	pos := g.positions()
	for _, depID := range res.Rewired {
		j := pos[depID]
		next.Tasks[j].Needs = replaceNeed(next.Tasks[j].Needs, id, replacements)
	}
	next.Tasks[i].State = State{Status: Dropped, Reason: reason}
	if err := adopt(g, next, "dropping", id); err != nil {
		return DropResult{}, err
	}
	res.Task = g.Tasks[i]
	return res, nil
}

// refusal builds the DropRefusal for id: its direct dependents and the size of
// the subtree with work ahead below them.
func (g *Graph) refusal(id string, deps []Dependent, now time.Time) *DropRefusal {
	direct := map[string]bool{}
	for _, d := range deps {
		if d.Blocks {
			direct[d.ID] = true
		}
	}
	transitive := 0
	for _, j := range g.subtree(id, now, g.neededBy()) {
		if !direct[g.Tasks[j].ID] {
			transitive++
		}
	}
	return &DropRefusal{ID: id, Dependents: deps, Transitive: transitive}
}

// DropCascade drops id and every task with work ahead that needs it, directly
// or through other tasks with work ahead, in one change with the same reason.
// Done tasks aren't dropped or descended through: they keep their need as
// history. It's refused, with nothing changed, if any task in the set holds a
// live claim; lapsed claims are cleared.
func (g *Graph) DropCascade(id, reason string, now time.Time) (DropResult, error) {
	i, err := g.find(id)
	if err != nil {
		return DropResult{}, err
	}
	t := g.Tasks[i]
	switch {
	case strings.TrimSpace(reason) == "":
		return DropResult{}, fmt.Errorf("dropping %q needs a reason (--reason)", id)
	case t.State.Status == Dropped:
		return DropResult{}, fmt.Errorf("task %q is already dropped (%s)", id, t.State.Reason)
	}
	by := g.neededBy()
	set := append([]int{i}, g.subtree(id, now, by)...)
	var claimed []string
	for _, j := range set {
		if x := g.Tasks[j]; x.Effective(now) == InProgress {
			claimed = append(claimed, fmt.Sprintf("%s (claimed by %q until %s)",
				x.ID, x.State.Claim.Holder, x.State.Claim.Expires.Format(time.RFC3339)))
		}
	}
	if len(claimed) > 0 {
		return DropResult{}, fmt.Errorf("can't cascade-drop %q: live claims in its subtree; release them first:\n  %s",
			id, strings.Join(claimed, "\n  "))
	}

	res := DropResult{Rewired: []string{}, KeptAsHistory: []string{}, Dependents: g.Dependents(id, now)}
	dropped := map[string]bool{}
	next := &Graph{Version: g.Version, Tasks: cloneTasks(g.Tasks)}
	for _, j := range set {
		next.Tasks[j].State = State{Status: Dropped, Reason: reason}
		dropped[g.Tasks[j].ID] = true
		res.Dropped = append(res.Dropped, g.Tasks[j].ID)
	}
	kept := map[int]bool{}
	for d := range dropped {
		for _, j := range by[d] {
			if g.Tasks[j].State.Status == Done {
				kept[j] = true
			}
		}
	}
	for j := range g.Tasks {
		if kept[j] {
			res.KeptAsHistory = append(res.KeptAsHistory, g.Tasks[j].ID)
		}
	}
	if err := adopt(g, next, "dropping", id); err != nil {
		return DropResult{}, err
	}
	res.Task = g.Tasks[i]
	return res, nil
}

// cloneTasks copies tasks deeply enough that editing the copy's slices never
// reaches the original.
func cloneTasks(tasks []Task) []Task {
	out := make([]Task, len(tasks))
	for i, t := range tasks {
		t.Needs = slices.Clone(t.Needs)
		t.Footprint = slices.Clone(t.Footprint)
		t.Done.Checks = slices.Clone(t.Done.Checks)
		out[i] = t
	}
	return out
}
