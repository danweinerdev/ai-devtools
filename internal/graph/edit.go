package graph

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Edit is the authored fields an update changes. A nil field is left alone;
// a non-nil list replaces the whole list. Status is never editable here. By
// and Reason are recorded in the task's history when the edit weakens its
// definition of done; they change nothing themselves.
type Edit struct {
	Title     *string
	Needs     *[]string
	Footprint *[]string
	Result    *string
	Checks    *[]string
	By        string
	Reason    string
}

// Weakening is how an edit weakens a task's definition of done: the checks
// it removes or changes, and whether it changes the result. A check list
// that keeps every existing check only adds proof, so it isn't one.
type Weakening struct {
	Removed []string
	Result  bool
}

// Weakens reports whether w weakens anything.
func (w Weakening) Weakens() bool { return len(w.Removed) > 0 || w.Result }

// String says what w weakens, for refusals and the guard's question.
func (w Weakening) String() string {
	var parts []string
	if len(w.Removed) > 0 {
		quoted := make([]string, len(w.Removed))
		for i, c := range w.Removed {
			quoted[i] = "`" + c + "`"
		}
		parts = append(parts, "removes or changes the check(s) "+strings.Join(quoted, ", "))
	}
	if w.Result {
		parts = append(parts, "changes the result")
	}
	return strings.Join(parts, " and ")
}

// WeakeningOf is how e would weaken t's definition of done.
func WeakeningOf(t Task, e Edit) Weakening {
	var w Weakening
	if e.Checks != nil {
		for _, c := range t.Done.Checks {
			if !slices.Contains(*e.Checks, c) {
				w.Removed = append(w.Removed, c)
			}
		}
	}
	w.Result = e.Result != nil && *e.Result != t.Done.Result
	return w
}

// Empty reports an edit that changes nothing; By and Reason don't count.
func (e Edit) Empty() bool {
	return e.Title == nil && e.Needs == nil && e.Footprint == nil && e.Result == nil && e.Checks == nil
}

// UpdateTask applies e to the task with id and returns the updated task. A
// dropped task is a record and can't be edited, and a task under a live claim
// can't be edited by anyone, its holder included, so the definition of done a
// holder is working to never changes under it. An edit that weakens the
// definition of done (see WeakeningOf) needs a reason and is recorded in the
// task's history, whatever the task's status. The edit is made on a copy
// and adopted only if the whole graph still validates (no cycle, no done or
// in-progress task gaining a need that isn't done), so every refusal leaves g as it
// was.
func (g *Graph) UpdateTask(id string, e Edit, now time.Time) (Task, error) {
	i, err := g.find(id)
	if err != nil {
		return Task{}, err
	}
	if g.Tasks[i].State.Status == Dropped {
		return Task{}, fmt.Errorf("task %q is dropped; a dropped task stays as a record and isn't edited", id)
	}
	if live(&g.Tasks[i], now) {
		return Task{}, fmt.Errorf("%s; release it first, then edit it", heldMsg(&g.Tasks[i]))
	}
	if e.Empty() {
		return Task{}, fmt.Errorf("nothing to update: pass at least one of --title, --needs, --footprint, --result, --check")
	}
	weak := WeakeningOf(g.Tasks[i], e)
	if weak.Weakens() && strings.TrimSpace(e.Reason) == "" {
		return Task{}, fmt.Errorf("updating %q %s, which weakens its definition of done; say why with --reason "+
			"(it's recorded in the task's history). Adding checks while keeping every existing one needs no reason", id, weak)
	}
	next := &Graph{Version: g.Version, Tasks: cloneTasks(g.Tasks)}
	t := &next.Tasks[i]
	if weak.Weakens() {
		c := Change{At: now.UTC(), By: e.By, Reason: strings.TrimSpace(e.Reason)}
		if len(weak.Removed) > 0 {
			c.ChecksFrom, c.ChecksTo = slices.Clone(t.Done.Checks), slices.Clone(*e.Checks)
		}
		if weak.Result {
			c.ResultFrom, c.ResultTo = t.Done.Result, *e.Result
		}
		t.History = append(t.History, c)
	}
	if e.Title != nil {
		t.Title = *e.Title
	}
	if e.Needs != nil {
		t.Needs = slices.Clone(*e.Needs)
	}
	if e.Footprint != nil {
		t.Footprint = slices.Clone(*e.Footprint)
	}
	if e.Result != nil {
		t.Done.Result = *e.Result
	}
	if e.Checks != nil {
		t.Done.Checks = slices.Clone(*e.Checks)
	}
	if err := adopt(g, next, "updating", id); err != nil {
		return Task{}, err
	}
	return g.Tasks[i], nil
}

// Split replaces the task id with parts, which must already exist as todo
// tasks. Each part inherits id's needs; every task that isn't dropped and
// needed id now needs all the parts instead; id is dropped with the reason
// "split into …". It returns every task it changed, split task first, in file
// order. The split is made on a copy and adopted only if the graph still
// validates, so every refusal leaves g as it was.
func (g *Graph) Split(id string, parts []string, now time.Time) ([]Task, error) {
	i, err := g.find(id)
	if err != nil {
		return nil, err
	}
	split := g.Tasks[i]
	switch st := split.Effective(now); st {
	case Done, Dropped:
		return nil, fmt.Errorf("task %q is %s; only a task that isn't done or dropped can be split", id, st)
	case InProgress:
		return nil, fmt.Errorf("task %q is claimed by %q until %s; release it first", id,
			split.State.Claim.Holder, split.State.Claim.Expires.Format(time.RFC3339))
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("split-task needs at least one part (--into)")
	}
	pos := g.positions()
	prereqs := g.ancestors(id, pos)
	partIdx := map[string]int{}
	for _, p := range parts {
		switch _, dup := partIdx[p]; {
		case p == id:
			return nil, fmt.Errorf("task %q can't be one of its own parts", id)
		case dup:
			return nil, fmt.Errorf("part %q is listed twice", p)
		}
		j, ok := pos[p]
		if !ok {
			return nil, fmt.Errorf("part %q doesn't exist; add the parts with add-task first", p)
		}
		pt := g.Tasks[j]
		if pt.State.Status != Todo {
			return nil, fmt.Errorf("part %q is %s; parts must be todo", p, pt.State.Status)
		}
		if slices.Contains(pt.Needs, id) {
			return nil, fmt.Errorf("part %q needs %q, the task being split; a part can't depend on what it replaces", p, id)
		}
		if prereqs[p] {
			return nil, fmt.Errorf("part %q is a prerequisite of %q (directly or through its needs); a part can't replace what the task it splits needs", p, id)
		}
		partIdx[p] = j
	}

	next := &Graph{Version: g.Version, Tasks: cloneTasks(g.Tasks)}
	changed := map[int]bool{i: true}
	for _, p := range parts {
		j := partIdx[p]
		next.Tasks[j].Needs = union(next.Tasks[j].Needs, split.Needs)
		changed[j] = true
	}
	for j := range next.Tasks {
		t := &next.Tasks[j]
		if j == i || t.State.Status == Dropped || !slices.Contains(t.Needs, id) {
			continue
		}
		t.Needs = replaceNeed(t.Needs, id, parts)
		changed[j] = true
	}
	next.Tasks[i].State = State{Status: Dropped, Reason: "split into " + strings.Join(parts, ", ")}
	if err := adopt(g, next, "splitting", id); err != nil {
		return nil, err
	}

	out := []Task{g.Tasks[i]}
	for j, t := range g.Tasks {
		if changed[j] && j != i {
			out = append(out, t)
		}
	}
	return out, nil
}

// adopt replaces g's tasks with next's if next validates; otherwise g is left
// alone and the error lists every problem the change would cause.
func adopt(g, next *Graph, op, id string) error {
	if problems := next.Validate(); len(problems) > 0 {
		return fmt.Errorf("%s %q would leave the graph invalid:\n  - %s", op, id, strings.Join(problems, "\n  - "))
	}
	g.Tasks = next.Tasks
	return nil
}

// ancestors is every task id needs, directly or through other needs.
func (g *Graph) ancestors(id string, pos map[string]int) map[string]bool {
	seen := map[string]bool{}
	queue := []string{id}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		j, ok := pos[cur]
		if !ok {
			continue
		}
		for _, n := range g.Tasks[j].Needs {
			if !seen[n] && n != id {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	return seen
}

// positions maps each ID to the index of its first task, so lookups inside a
// loop stay constant time.
func (g *Graph) positions() map[string]int {
	pos := make(map[string]int, len(g.Tasks))
	for i, t := range g.Tasks {
		if _, ok := pos[t.ID]; !ok {
			pos[t.ID] = i
		}
	}
	return pos
}

// find is the index of id, or an error.
func (g *Graph) find(id string) (int, error) {
	for i, t := range g.Tasks {
		if t.ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("no task %q", id)
}

// union is a followed by the items of b it lacks, without duplicates.
func union(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	seen := make(map[string]bool, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// replaceNeed swaps old for with at old's position, dropping duplicates.
func replaceNeed(needs []string, old string, with []string) []string {
	var out []string
	for _, n := range needs {
		if n == old {
			out = append(out, with...)
		} else {
			out = append(out, n)
		}
	}
	return union(out, nil)
}
