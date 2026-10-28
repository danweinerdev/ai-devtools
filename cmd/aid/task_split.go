package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, splitTaskCmd) }

func splitTaskCmd(a *app) *cobra.Command {
	var parts []string
	cmd := &cobra.Command{
		Use:   "split-task <id> --into <ids>",
		Short: "Replace a task with parts that already exist",
		Long: "Replace a task that isn't done with parts added beforehand with add-task. The parts inherit its needs, " +
			"every task that needed it now needs all the parts, and it's dropped with the reason \"split into …\", in one write.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, _, err := a.open()
			if err != nil {
				return err
			}
			now := a.now()
			var changed []graph.Task
			err = w.Update(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				if changed, err = g.Split(args[0], parts, now); err != nil {
					return err
				}
				return saveGraph(tx, g)
			})
			if err != nil {
				return err
			}
			views := make([]taskView, 0, len(changed))
			var rewired []string
			for _, t := range changed {
				views = append(views, view(t, now))
				if t.ID != args[0] && !slices.Contains(parts, t.ID) {
					rewired = append(rewired, t.ID)
				}
			}
			text := fmt.Sprintf("split %s into %s (dropped)", args[0], strings.Join(parts, ", "))
			if len(rewired) > 0 {
				text += "\nnow needing the parts: " + strings.Join(rewired, ", ")
			}
			return a.emit(views, text)
		},
	}
	cmd.Flags().StringSliceVar(&parts, "into", []string{}, "the part IDs, comma-separated or repeated (required)")
	_ = cmd.MarkFlagRequired("into")
	return cmd
}
