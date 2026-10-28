package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, showCmd) }

// statusOrder is how show's summary lists the statuses.
var statusOrder = []string{graph.Todo, graph.InProgress, graph.Blocked, graph.Done, graph.Dropped}

// itemCounts is the state record in brief.
type itemCounts struct {
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
}

// graphView is `show --json` without IDs.
type graphView struct {
	Tasks []taskView `json:"tasks"`
	Items itemCounts `json:"items"`
}

// taskDetail is `show <id> --json`: the task plus who needs it.
type taskDetail struct {
	taskView
	Dependents []string `json:"dependents"`
}

func showCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "show [ids…]",
		Short: "Print the graph, or the given tasks in full",
		Long: "Without IDs, print every task in file order with its effective status and needs, then a " +
			"summary. With IDs, print each of those tasks in full, including the tasks that need it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			w, _, err := a.open()
			if err != nil {
				return err
			}
			now := a.now()
			var g *graph.Graph
			var r *state.Record
			err = w.View(func(tx *store.Tx) error {
				if g, err = loadGraph(tx); err != nil {
					return err
				}
				r, err = loadState(tx)
				return err
			})
			if err != nil {
				return err
			}
			if len(args) == 0 {
				return showGraph(a, g, r, now)
			}
			return showTasks(a, g, args, now)
		},
	}
}

func showGraph(a *app, g *graph.Graph, r *state.Record, now time.Time) error {
	v := graphView{Tasks: []taskView{}}
	var b strings.Builder
	counts := map[string]int{}
	for _, t := range g.Tasks {
		tv := view(t, now)
		v.Tasks = append(v.Tasks, tv)
		counts[tv.Effective]++
		fmt.Fprintf(&b, "%s\t%s\t%s", t.ID, tv.Effective, t.Title)
		if len(t.Needs) > 0 {
			fmt.Fprintf(&b, "\t[needs: %s]", strings.Join(t.Needs, ", "))
		}
		b.WriteString("\n")
	}
	for _, it := range r.Items {
		if it.Status == state.Open {
			v.Items.Open++
		} else {
			v.Items.Resolved++
		}
	}
	if len(g.Tasks) == 0 {
		b.WriteString("no tasks\n")
	}
	var parts []string
	for _, s := range statusOrder {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	summary := fmt.Sprintf("%d tasks", len(g.Tasks))
	if len(parts) > 0 {
		summary += " (" + strings.Join(parts, ", ") + ")"
	}
	fmt.Fprintf(&b, "%s; %d open items, %d resolved", summary, v.Items.Open, v.Items.Resolved)
	return a.emit(v, b.String())
}

func showTasks(a *app, g *graph.Graph, ids []string, now time.Time) error {
	details := make([]taskDetail, 0, len(ids))
	var b strings.Builder
	for n, id := range ids {
		t, ok := g.ByID(id)
		if !ok {
			_, err := index(g, id)
			return err
		}
		d := taskDetail{taskView: view(t, now), Dependents: dependents(g, id)}
		details = append(details, d)
		if n > 0 {
			b.WriteString("\n")
		}
		writeDetail(&b, d)
	}
	return a.emit(details, b.String())
}

// dependents lists the tasks that need id, in file order.
func dependents(g *graph.Graph, id string) []string {
	deps := []string{}
	for _, t := range g.Tasks {
		for _, n := range t.Needs {
			if n == id {
				deps = append(deps, t.ID)
				break
			}
		}
	}
	return deps
}

func writeDetail(b *strings.Builder, d taskDetail) {
	list := func(xs []string) string {
		if len(xs) == 0 {
			return "none"
		}
		return strings.Join(xs, ", ")
	}
	fmt.Fprintf(b, "%s: %s\n", d.ID, d.Title)
	status := d.Effective
	if d.Effective != d.State.Status {
		status += " (lease lapsed; stored as " + d.State.Status + ")"
	}
	fmt.Fprintf(b, "  status:     %s\n", status)
	if !d.State.Claim.IsZero() {
		fmt.Fprintf(b, "  claim:      %s until %s\n", d.State.Claim.Holder, d.State.Claim.Expires.Format(time.RFC3339))
	}
	if d.State.Reason != "" {
		fmt.Fprintf(b, "  reason:     %s\n", d.State.Reason)
	}
	fmt.Fprintf(b, "  needs:      %s\n", list(d.Needs))
	fmt.Fprintf(b, "  needed by:  %s\n", list(d.Dependents))
	fmt.Fprintf(b, "  footprint:  %s\n", list(d.Footprint))
	fmt.Fprintf(b, "  result:     %s\n", d.Done.Result)
	b.WriteString("  checks:\n")
	for _, c := range d.Done.Checks {
		fmt.Fprintf(b, "    %s\n", c)
	}
}
