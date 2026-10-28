package main

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/checks"
	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, completeCmd) }

// checkView is one check's outcome as commands print it.
type checkView struct {
	Command  string  `json:"command"`
	Passed   bool    `json:"passed"`
	ExitCode int     `json:"exitCode"`
	TimedOut bool    `json:"timedOut,omitempty"`
	Error    string  `json:"error,omitempty"`
	Output   string  `json:"output,omitempty"`
	Seconds  float64 `json:"seconds"`
}

func checkViews(results []checks.Result) ([]checkView, bool) {
	views := make([]checkView, 0, len(results))
	all := true
	for _, r := range results {
		all = all && r.Passed
		v := checkView{Command: r.Command, Passed: r.Passed, ExitCode: r.ExitCode, TimedOut: r.TimedOut,
			Error: r.Err, Seconds: math.Round(r.Duration.Seconds()*10) / 10}
		if !r.Passed {
			v.Output = r.Output // passing output is noise
		}
		views = append(views, v)
	}
	return views, all
}

func checkText(views []checkView) string {
	var b strings.Builder
	for _, v := range views {
		mark := "pass"
		if !v.Passed {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "  %s  %s (%.1fs)\n", mark, v.Command, v.Seconds)
		if !v.Passed {
			if v.Error != "" {
				fmt.Fprintf(&b, "        %s\n", v.Error)
			}
			for _, line := range strings.Split(strings.TrimRight(v.Output, "\n"), "\n") {
				if line != "" {
					fmt.Fprintf(&b, "        %s\n", line)
				}
			}
		}
	}
	return b.String()
}

func completeCmd(a *app) *cobra.Command {
	var holder string
	cmd := &cobra.Command{
		Use:   "complete-task <id>",
		Short: "Run an in-progress task's checks; mark it done only if they all pass",
		Long: "Run the task's checks from the repository root and mark it done only if every one passes. " +
			"The checks run without holding the lock, so other commands aren't blocked; the task is then " +
			"marked done only if it's still held by --holder with the same checks.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, root, err := a.open()
			if err != nil {
				return err
			}
			id := args[0]
			holder := holderOf(holder)
			var want []string
			err = w.View(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				t, err := g.HeldBy(id, holder, a.now())
				if err != nil {
					return err
				}
				want = t.Done.Checks
				return nil
			})
			if err != nil {
				return err
			}

			views, passed := checkViews(checks.RunAll(cmd.Context(), root, want, 0))
			result := map[string]any{"id": id, "done": false, "checks": views}
			if !passed {
				return &refusal{msg: fmt.Sprintf("task %q stays in-progress: a check failed\n%s", id, checkText(views)), detail: result}
			}

			err = w.Update(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				i, err := index(g, id)
				if err != nil {
					return err
				}
				t := &g.Tasks[i]
				// A lease that lapsed while the checks ran is fine as long as
				// nobody else claimed the task in the meantime.
				if t.State.Status != graph.InProgress || t.State.Claim.Holder != holder {
					return fmt.Errorf("task %q changed hands while its checks ran; not marked done", id)
				}
				if !slices.Equal(t.Done.Checks, want) {
					return fmt.Errorf("task %q's checks changed while they ran; run complete-task again", id)
				}
				t.State = graph.State{Status: graph.Done}
				return saveGraph(tx, g)
			})
			if err != nil {
				return err
			}
			result["done"] = true
			return a.emit(result, fmt.Sprintf("done %s\n%s", id, checkText(views)))
		},
	}
	holderFlag(cmd, &holder, "the holder that claimed it")
	return cmd
}
