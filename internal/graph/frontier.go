package graph

import "time"

// Effective is the task's status at now. An in-progress task whose lease expired at or
// before now counts as todo: a lapsed lease releases itself, without anyone
// having to write the release.
func (t Task) Effective(now time.Time) string {
	if t.State.Status == InProgress && !t.State.Claim.Expires.After(now) {
		return Todo
	}
	return t.State.Status
}

// Ready is the frontier at now: tasks that are effectively todo and whose
// needs are all done, in file order.
func (g *Graph) Ready(now time.Time) []Task {
	status := map[string]string{}
	for _, t := range g.Tasks {
		status[t.ID] = t.State.Status
	}
	var ready []Task
	for _, t := range g.Tasks {
		if t.Effective(now) != Todo {
			continue
		}
		ok := true
		for _, n := range t.Needs {
			if status[n] != Done {
				ok = false
				break
			}
		}
		if ok {
			ready = append(ready, t)
		}
	}
	return ready
}
