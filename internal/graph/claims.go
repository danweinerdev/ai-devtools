package graph

import (
	"fmt"
	"strings"
	"time"
)

// The status changes other than complete and drop. Each takes the current
// time from its caller, edits the graph in place, and returns the changed
// task; the caller validates and saves.

// task returns a pointer to the task with id, or an error that says how many
// tasks there are so a typo is obvious.
func (g *Graph) task(id string) (*Task, error) {
	for i := range g.Tasks {
		if g.Tasks[i].ID == id {
			return &g.Tasks[i], nil
		}
	}
	return nil, fmt.Errorf("no task %q (%d tasks; `aid show` lists them)", id, len(g.Tasks))
}

// live reports whether t has a claim that hasn't lapsed at now.
func live(t *Task, now time.Time) bool {
	return t.State.Status == InProgress && t.State.Claim.Expires.After(now)
}

func heldMsg(t *Task) string {
	return fmt.Sprintf("task %q is held by %q until %s", t.ID, t.State.Claim.Holder,
		t.State.Claim.Expires.Format(time.RFC3339))
}

// Claim marks a ready task in-progress, held by holder until now+lease. A lapsed
// lease counts as released; the same holder claiming a live claim renews it.
func (g *Graph) Claim(id, holder string, lease time.Duration, now time.Time) (Task, error) {
	if holder == "" {
		return Task{}, fmt.Errorf("a claim needs a holder")
	}
	if lease <= 0 {
		return Task{}, fmt.Errorf("the lease must be positive")
	}
	t, err := g.task(id)
	if err != nil {
		return Task{}, err
	}
	switch st := t.Effective(now); {
	case st == InProgress && t.State.Claim.Holder != holder:
		return Task{}, fmt.Errorf("%s", heldMsg(t))
	case st != Todo && st != InProgress:
		return Task{}, fmt.Errorf("task %q is %s; only todo tasks can be claimed", t.ID, st)
	}
	if unmet := g.unmetNeeds(*t, now); len(unmet) > 0 {
		return Task{}, fmt.Errorf("task %q isn't ready; it needs %s", t.ID, strings.Join(unmet, ", "))
	}
	t.State = State{Status: InProgress, Claim: Claim{Holder: holder, Expires: now.Add(lease).UTC()}}
	return *t, nil
}

// Release hands an in-progress task back (todo, claim cleared). Only its holder may
// release a live claim; a lapsed one is already effectively todo, so
// releasing it just clears the stale claim. Releasing a blocked task unblocks
// it: that's how blocked work resumes.
func (g *Graph) Release(id, holder string, now time.Time) (Task, error) {
	t, err := g.task(id)
	if err != nil {
		return Task{}, err
	}
	switch t.State.Status {
	case InProgress:
		if live(t, now) && t.State.Claim.Holder != holder {
			return Task{}, fmt.Errorf("%s; only its holder can release it", heldMsg(t))
		}
	case Blocked:
	default:
		return Task{}, fmt.Errorf("task %q is %s; only in-progress or blocked tasks can be released", t.ID, t.State.Status)
	}
	t.State = State{Status: Todo}
	return *t, nil
}

// Block marks a todo or in-progress task blocked, with the reason, and clears any
// claim. A live claim can only be blocked by its holder.
func (g *Graph) Block(id, reason, holder string, now time.Time) (Task, error) {
	if strings.TrimSpace(reason) == "" {
		return Task{}, fmt.Errorf("blocking a task needs a reason")
	}
	t, err := g.task(id)
	if err != nil {
		return Task{}, err
	}
	switch t.State.Status {
	case Todo, InProgress:
	default:
		return Task{}, fmt.Errorf("task %q is %s; only todo or in-progress tasks can be blocked", t.ID, t.State.Status)
	}
	if live(t, now) && t.State.Claim.Holder != holder {
		return Task{}, fmt.Errorf("%s; only its holder can block it", heldMsg(t))
	}
	t.State = State{Status: Blocked, Reason: reason}
	return *t, nil
}

// HeldBy refuses unless the task is in-progress and claimed by holder. A lapsed
// lease still counts for its own holder until someone else claims the task.
func (g *Graph) HeldBy(id, holder string, now time.Time) (Task, error) {
	t, err := g.task(id)
	if err != nil {
		return Task{}, err
	}
	if t.State.Status != InProgress {
		return Task{}, fmt.Errorf("task %q is %s; claim it first", t.ID, t.Effective(now))
	}
	if t.State.Claim.Holder != holder {
		return Task{}, fmt.Errorf("task %q is held by %q, not %q", t.ID, t.State.Claim.Holder, holder)
	}
	return *t, nil
}

// unmetNeeds lists the needs of t that aren't done, as "id (status)". A need
// that doesn't exist is unmet too, so claiming agrees with Ready.
func (g *Graph) unmetNeeds(t Task, now time.Time) []string {
	var unmet []string
	for _, n := range t.Needs {
		dep, ok := g.ByID(n)
		switch {
		case !ok:
			unmet = append(unmet, n+" (missing)")
		case dep.State.Status != Done:
			unmet = append(unmet, fmt.Sprintf("%s (%s)", n, dep.Effective(now)))
		}
	}
	return unmet
}
