package main

import (
	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, releaseCmd) }

func releaseCmd(a *app) *cobra.Command {
	var holder string
	cmd := &cobra.Command{
		Use:   "release-task <id>",
		Short: "Hand an in-progress task back to todo, or unblock a blocked one",
		Long: "Hand an in-progress task back to todo and clear its claim; only its holder may release a live claim " +
			"(a lapsed one is already effectively todo, and releasing it clears the stale claim). " +
			"Releasing a blocked task unblocks it back to todo and clears the reason: that's how blocked work resumes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.changeTask(func(g *graph.Graph) (graph.Task, error) {
				return g.Release(args[0], holderOf(holder), a.now())
			}, "released")
		},
	}
	holderFlag(cmd, &holder, "the holder of the claim (matters for a live claim)")
	return cmd
}

// changeTask applies one status change under the write lock and reports the
// changed task.
func (a *app) changeTask(change func(*graph.Graph) (graph.Task, error), verb string) error {
	w, _, err := a.open()
	if err != nil {
		return err
	}
	var changed graph.Task
	err = w.Update(func(tx *store.Tx) error {
		g, err := loadGraph(tx)
		if err != nil {
			return err
		}
		if changed, err = change(g); err != nil {
			return err
		}
		return saveGraph(tx, g)
	})
	if err != nil {
		return err
	}
	return a.emit(view(changed, a.now()), verb+" "+changed.ID+" ("+changed.State.Status+")")
}
