package main

import (
	"github.com/danweinerdev/ai-devtools/internal/graph"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, blockCmd) }

func blockCmd(a *app) *cobra.Command {
	var reason, holder string
	cmd := &cobra.Command{
		Use:   "block-task <id>",
		Short: "Mark a todo or in-progress task blocked, with a reason",
		Long: "Mark a todo or in-progress task blocked, with the reason, and clear its claim. A live claim can only be " +
			"blocked by its holder. `release-task` unblocks it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.changeTask(func(g *graph.Graph) (graph.Task, error) {
				return g.Block(args[0], reason, holderOf(holder), a.now())
			}, "blocked")
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the work can't go on (required)")
	holderFlag(cmd, &holder, "the holder of the claim (matters for a live claim)")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}
