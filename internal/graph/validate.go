package graph

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// drivePattern is a Windows drive root: a letter and a colon, alone or before
// a slash (`C:`, `c:/x`). `a:b` is just a relative name with a colon.
var drivePattern = regexp.MustCompile(`^[A-Za-z]:(/|$)`)

func isDrive(p string) bool { return drivePattern.MatchString(p) }

// idPattern admits readable slugs and lowercase UUIDs alike.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Validate reports every rule the graph breaks, each located by task. It
// implements store.Validator, so it runs on every load and before every write.
func (g *Graph) Validate() []string {
	var problems []string
	add := func(at, format string, args ...any) {
		problems = append(problems, at+": "+fmt.Sprintf(format, args...))
	}

	byID := map[string]Task{}
	for i, t := range g.Tasks {
		at := fmt.Sprintf("task `%s`", t.ID)
		switch {
		case !idPattern.MatchString(t.ID):
			add(fmt.Sprintf("tasks[%d]", i), "id %q must match %s", t.ID, idPattern)
		case byID[t.ID].ID != "":
			add(at, "duplicate id")
		default:
			byID[t.ID] = t
		}
		if strings.TrimSpace(t.Title) == "" {
			add(at, "title is empty")
		}
		if strings.TrimSpace(t.Done.Result) == "" {
			add(at, "done.result is empty")
		}
		if len(t.Done.Checks) == 0 {
			add(at, "done.checks needs at least one command")
		}
		for j, c := range t.Done.Checks {
			if strings.TrimSpace(c) == "" {
				add(at, "done.checks[%d] is empty", j)
			}
		}
		if len(t.Footprint) == 0 {
			add(at, "footprint needs at least one path")
		}
		for _, p := range t.Footprint {
			if msg := footprintProblem(p); msg != "" {
				add(at, "footprint %q %s", p, msg)
			}
		}
		validateState(t, func(format string, args ...any) { add(at, format, args...) })
		for j, c := range t.History {
			validateChange(c, func(format string, args ...any) { add(at, "history[%d] %s", j, fmt.Sprintf(format, args...)) })
		}
	}

	for _, t := range g.Tasks {
		at := fmt.Sprintf("task `%s`", t.ID)
		seen := map[string]bool{}
		for _, n := range t.Needs {
			dep, exists := byID[n]
			switch {
			case n == t.ID:
				add(at, "needs itself")
			case seen[n]:
				add(at, "needs `%s` twice", n)
			case !exists:
				add(at, "needs `%s`, which doesn't exist", n)
			case workAhead(t) && dep.State.Status == Dropped:
				add(at, "needs `%s`, which is dropped; move or remove the need", n)
			case t.State.Status == Done && dep.State.Status != Done && dep.State.Status != Dropped:
				// A done task may keep a need on a dropped one: it records
				// what the task was built on.
				add(at, "is done but needs `%s`, which is %s", n, dep.State.Status)
			case t.State.Status == InProgress && dep.State.Status != Done:
				// It was claimed with every need done; a need that isn't
				// would leave its holder unable to renew or complete it.
				add(at, "is in-progress but needs `%s`, which is %s; release it before rewiring its needs", n, dep.State.Status)
			}
			seen[n] = true
		}
	}

	return append(problems, cycleProblems(g.Tasks)...)
}

// workAhead reports a task that still has work ahead of it: todo, in-progress or
// blocked.
func workAhead(t Task) bool {
	return t.State.Status == Todo || t.State.Status == InProgress || t.State.Status == Blocked
}

func validateState(t Task, add func(format string, args ...any)) {
	s := t.State
	if !slices.Contains(statuses, s.Status) {
		add("status %q must be one of %s", s.Status, strings.Join(statuses, ", "))
		return
	}
	switch {
	case s.Status == InProgress && (s.Claim.Holder == "" || s.Claim.Expires.IsZero()):
		add("is in-progress but has no claim with a holder and an expiry")
	case s.Status != InProgress && !s.Claim.IsZero():
		add("is %s but carries a claim; only in-progress tasks are claimed", s.Status)
	}
	needsReason := s.Status == Blocked || s.Status == Dropped
	switch {
	case needsReason && strings.TrimSpace(s.Reason) == "":
		add("is %s without a reason", s.Status)
	case !needsReason && s.Reason != "":
		add("is %s but carries a reason; only blocked and dropped tasks have one", s.Status)
	}
}

// footprintProblem says what's wrong with a footprint path, or "".
func footprintProblem(p string) string {
	clean := path.Clean(p)
	switch {
	case p == "":
		return "is empty"
	case strings.Contains(p, `\`):
		return "uses backslashes; use / on every platform"
	case strings.HasPrefix(p, "/") || isDrive(p):
		return "is absolute; use a path relative to the repository root"
	case clean == ".." || strings.HasPrefix(clean, "../"):
		// Checked before cleanliness, so the advice below is never a path
		// that's then refused for leaving the repository.
		return "leaves the repository"
	case p != clean && p != clean+"/":
		return "isn't clean; write it as " + fmt.Sprintf("%q", clean)
	}
	return ""
}

// cycleProblems reports each knot of mutually dependent tasks (a strongly
// connected component) once: one concrete loop through it, starting at its
// earliest task in file order, plus any other tasks caught in the same knot.
// Self needs are reported on their own ("needs itself"), and needs that don't
// exist are ignored here. With duplicate IDs the needs of every copy count, so
// a loop through a later copy still shows.
func cycleProblems(tasks []Task) []string {
	// Nodes are the distinct IDs in file order; edges are their needs, in the
	// order they're listed.
	idx := map[string]int{}
	var ids []string
	for _, t := range tasks {
		if _, ok := idx[t.ID]; !ok {
			idx[t.ID] = len(ids)
			ids = append(ids, t.ID)
		}
	}
	edges := make([][]int, len(ids))
	for _, t := range tasks {
		from := idx[t.ID]
		for _, n := range t.Needs {
			if to, ok := idx[n]; ok && to != from && !slices.Contains(edges[from], to) {
				edges[from] = append(edges[from], to)
			}
		}
	}

	var out []string
	for _, comp := range components(edges) {
		if len(comp) < 2 {
			continue
		}
		loop := shortestLoop(comp, edges)
		names := make([]string, len(loop))
		onLoop := map[int]bool{}
		for i, v := range loop {
			names[i] = ids[v]
			onLoop[v] = true
		}
		msg := "cycle: " + strings.Join(names, " → ")
		var others []string
		for _, v := range comp {
			if !onLoop[v] {
				others = append(others, "`"+ids[v]+"`")
			}
		}
		if len(others) > 0 {
			msg += " (also involves " + strings.Join(others, ", ") + ")"
		}
		out = append(out, msg)
	}
	return out
}

// components is Tarjan's strongly connected components, run iteratively so a
// long chain can't exhaust the stack. Each component's nodes are sorted (file
// order), and components are ordered by their first node.
func components(edges [][]int) [][]int {
	n := len(edges)
	index := make([]int, n)
	low := make([]int, n)
	onStack := make([]bool, n)
	for i := range index {
		index[i] = -1
	}
	var comps [][]int
	var stack []int
	next := 0
	type frame struct{ v, edge int }
	visit := func(v int) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
	}
	for root := range n {
		if index[root] >= 0 {
			continue
		}
		visit(root)
		call := []frame{{root, 0}}
		for len(call) > 0 {
			f := &call[len(call)-1]
			if f.edge < len(edges[f.v]) {
				w := edges[f.v][f.edge]
				f.edge++
				switch {
				case index[w] < 0:
					visit(w)
					call = append(call, frame{w, 0})
				case onStack[w]:
					low[f.v] = min(low[f.v], index[w])
				}
				continue
			}
			v := f.v
			call = call[:len(call)-1]
			if len(call) > 0 {
				parent := call[len(call)-1].v
				low[parent] = min(low[parent], low[v])
			}
			if low[v] != index[v] {
				continue
			}
			var comp []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			slices.Sort(comp)
			comps = append(comps, comp)
		}
	}
	slices.SortFunc(comps, func(a, b []int) int { return a[0] - b[0] })
	return comps
}

// shortestLoop is a shortest loop from the component's first node back to
// itself, staying inside the component and trying needs in listed order, as
// start → … → start.
func shortestLoop(comp []int, edges [][]int) []int {
	start := comp[0]
	in := map[int]bool{}
	for _, v := range comp {
		in[v] = true
	}
	prev := map[int]int{start: -1}
	queue := []int{start}
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		for _, w := range edges[v] {
			if !in[w] {
				continue
			}
			if w == start {
				var back []int
				for at := v; at != start; at = prev[at] {
					back = append(back, at)
				}
				slices.Reverse(back)
				return append(append([]int{start}, back...), start)
			}
			if _, seen := prev[w]; !seen {
				prev[w] = v
				queue = append(queue, w)
			}
		}
	}
	return []int{start, start} // unreachable: a component of two or more has a loop
}

// validateChange checks one history entry: who, when and why, and at least
// one change with both its old and new value.
func validateChange(c Change, add func(format string, args ...any)) {
	if c.At.IsZero() {
		add("has no time (`at`)")
	}
	if strings.TrimSpace(c.By) == "" {
		add("says nothing about who made it (`by`)")
	}
	if strings.TrimSpace(c.Reason) == "" {
		add("gives no reason")
	}
	checks := len(c.ChecksFrom) > 0 || len(c.ChecksTo) > 0
	result := c.ResultFrom != "" || c.ResultTo != ""
	switch {
	case !checks && !result:
		add("records no change to the checks or the result")
	case checks && (len(c.ChecksFrom) == 0 || len(c.ChecksTo) == 0):
		add("needs both `checks_from` and `checks_to`")
	case result && (c.ResultFrom == "" || c.ResultTo == ""):
		add("needs both `result_from` and `result_to`")
	}
}
