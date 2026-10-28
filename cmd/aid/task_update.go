package main

import (
	"fmt"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, updateTaskCmd) }

func updateTaskCmd(a *app) *cobra.Command {
	var title, result string
	var needs, footprint, checks []string
	cmd := &cobra.Command{
		Use:   "update-task <id>",
		Short: "Change a task's title, needs, footprint, result, or checks",
		Long: "Change a task's authored fields. Only the flags given change; a list flag replaces the whole list. " +
			"Status never changes here. A dropped task can't be edited, and neither can a task under a live claim, " +
			"its holder included: release it first.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var e graph.Edit
			var fields []string
			changed := func(name string) bool {
				if cmd.Flags().Changed(name) {
					fields = append(fields, name)
					return true
				}
				return false
			}
			if changed("title") {
				e.Title = &title
			}
			if changed("needs") {
				e.Needs = &needs
			}
			if changed("footprint") {
				e.Footprint = &footprint
			}
			if changed("result") {
				e.Result = &result
			}
			if changed("check") {
				e.Checks = &checks
			}
			w, _, err := a.open()
			if err != nil {
				return err
			}
			now := a.now()
			var updated graph.Task
			err = w.Update(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				if updated, err = g.UpdateTask(args[0], e, now); err != nil {
					return err
				}
				return saveGraph(tx, g)
			})
			if err != nil {
				return err
			}
			return a.emit(view(updated, now), fmt.Sprintf("updated %s: %s", updated.ID, strings.Join(fields, ", ")))
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "new title")
	f.StringSliceVar(&needs, "needs", []string{}, "new needs, replacing the list (pass --needs= to clear)")
	f.StringSliceVar(&footprint, "footprint", []string{}, "new footprint, replacing the list")
	f.StringVar(&result, "result", "", "new measurable result")
	f.StringArrayVar(&checks, "check", []string{}, "new checks, replacing the list; repeat for several")
	return cmd
}
