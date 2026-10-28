package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, dropCmd) }

func dropCmd(a *app) *cobra.Command {
	var reason string
	var replacedBy []string
	var cascade bool
	cmd := &cobra.Command{
		Use:   "drop-task <id>",
		Short: "Mark a task dropped, with a reason; it stays in the graph as a record",
		Long: "Mark a task dropped with a reason. It stays in the graph as a record, leaves the check batch, " +
			"and stops counting toward finishing. It's refused while a claim is live, and while a task with work " +
			"ahead (todo, in-progress or blocked) still needs it: the refusal lists the direct dependents, whose needs " +
			"must move, and counts the tasks with work ahead below them. Decide each and drop again, or handle the " +
			"whole subtree: --replaced-by moves every such direct dependent's need onto the replacements, and " +
			"--cascade drops the task and every task with work ahead below it, in one write with the same reason " +
			"(refused if any of them holds a live claim). Dependents that are already done aren't reopened: they " +
			"keep their need as a record of what they were built on.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cascade && len(replacedBy) > 0 {
				return fmt.Errorf("--cascade and --replaced-by don't mix: either drop the whole subtree or reassign it")
			}
			w, _, err := a.open()
			if err != nil {
				return err
			}
			now := a.now()
			var res graph.DropResult
			err = w.Update(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				if cascade {
					res, err = g.DropCascade(args[0], reason, now)
				} else {
					res, err = g.Drop(args[0], reason, replacedBy, now)
				}
				if err != nil {
					return err
				}
				return saveGraph(tx, g)
			})
			var dr *graph.DropRefusal
			if errors.As(err, &dr) {
				return &refusal{msg: dr.Error(), detail: dr}
			}
			if err != nil {
				return err
			}
			return a.emit(res, dropText(res))
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why it no longer applies (required)")
	cmd.Flags().StringSliceVar(&replacedBy, "replaced-by", []string{}, "tasks that replace it: every direct dependent with work ahead now needs these instead")
	cmd.Flags().BoolVar(&cascade, "cascade", false, "also drop every task with work ahead that needs it, directly or in turn")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

// dropText reports the drop and how each dependent was resolved, which the
// orchestrator relays to the user.
func dropText(res graph.DropResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "dropped %s: %s\n", res.Task.ID, res.Task.State.Reason)
	if len(res.Dropped) > 1 {
		fmt.Fprintf(&b, "  cascaded to: %s\n", strings.Join(res.Dropped[1:], ", "))
	}
	if len(res.Rewired) > 0 {
		fmt.Fprintf(&b, "  rewired onto the replacements: %s\n", strings.Join(res.Rewired, ", "))
	}
	if len(res.KeptAsHistory) > 0 {
		fmt.Fprintf(&b, "  done, keeping their need as history: %s\n", strings.Join(res.KeptAsHistory, ", "))
	}
	return b.String()
}
