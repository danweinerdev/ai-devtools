package main

import (
	"fmt"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, finishCmd) }

// leftTask is a task that keeps the work from finishing.
type leftTask struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Holder string `json:"holder,omitempty"`
}

// weakenedTask is a task whose definition of done was weakened, with how.
type weakenedTask struct {
	ID      string         `json:"id"`
	History []graph.Change `json:"history"`
}

// afterFinishScan runs at the end of each pass of finish's locked scan. Tests
// use it to change a file mid-transaction and force a retry.
var afterFinishScan = func() {}

func finishCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "finish",
		Short: "Succeed only when every task is done or dropped and nothing is open",
		Long: "Succeed only when every task is done or dropped, no claim is live, and no state item is open. " +
			"The check runs inside the locked write, so the answer reflects one consistent snapshot of both files. " +
			"On success it lists every task whose definition of done was weakened, with its history, for review.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, _, err := a.open()
			if err != nil {
				return err
			}
			now := a.now()
			var left []leftTask
			var open []state.Item
			var weakened []weakenedTask
			done, dropped := 0, 0
			err = w.Update(func(tx *store.Tx) error {
				// Update re-runs this closure when a file changed under the
				// lock, so every tally starts fresh on each run.
				left, open, weakened, done, dropped = nil, nil, nil, 0, 0
				defer afterFinishScan()
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				for _, t := range g.Tasks {
					if len(t.History) > 0 {
						weakened = append(weakened, weakenedTask{ID: t.ID, History: t.History})
					}
					switch t.State.Status {
					case graph.Done:
						done++
					case graph.Dropped:
						dropped++
					default:
						left = append(left, leftTask{ID: t.ID, Status: t.Effective(now), Holder: t.State.Claim.Holder})
					}
				}
				r, err := loadState(tx)
				if err != nil {
					return err
				}
				for _, it := range r.Items {
					if it.Status == state.Open {
						open = append(open, it)
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			// An empty graph is a missed step, not finished work: finish is the
			// gate before the rebuild drops .aid/.
			if done+dropped+len(left) == 0 {
				return &refusal{
					msg:    "not finished: no tasks; the graph was never built (add-task)",
					detail: map[string]any{"tasks": []leftTask{}, "items": []state.Item{}},
				}
			}
			if len(left) > 0 || len(open) > 0 {
				var lines []string
				if len(left) > 0 {
					lines = append(lines, fmt.Sprintf("%d task(s) left:", len(left)))
				}
				for _, l := range left {
					line := "  " + l.ID + " (" + l.Status
					if l.Holder != "" {
						line += ", claimed by " + l.Holder
					}
					lines = append(lines, line+")")
				}
				if len(open) > 0 {
					lines = append(lines, fmt.Sprintf("%d item(s) open:", len(open)))
				}
				for _, it := range open {
					lines = append(lines, "  "+it.ID+" ("+it.Kind+") "+it.Text)
				}
				if left == nil {
					left = []leftTask{}
				}
				if open == nil {
					open = []state.Item{}
				}
				return &refusal{
					msg:    "not finished\n" + strings.Join(lines, "\n"),
					detail: map[string]any{"tasks": left, "items": open},
				}
			}
			text := fmt.Sprintf("finished: %d done, %d dropped", done, dropped)
			if len(weakened) > 0 {
				var b strings.Builder
				fmt.Fprintf(&b, "\n%d task(s) had their definition of done weakened; review each:\n", len(weakened))
				for _, w := range weakened {
					fmt.Fprintf(&b, "  %s\n", w.ID)
					writeHistory(&b, w.History, "    ")
				}
				text += strings.TrimRight(b.String(), "\n")
			}
			if weakened == nil {
				weakened = []weakenedTask{}
			}
			return a.emit(map[string]any{"done": done, "dropped": dropped, "weakened": weakened}, text)
		},
	}
}
