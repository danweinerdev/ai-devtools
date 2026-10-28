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
	var title, result, reason, holder string
	var needs, footprint, checks []string
	cmd := &cobra.Command{
		Use:   "update-task <id>",
		Short: "Change a task's title, needs, footprint, result, or checks",
		Long: "Change a task's authored fields. Only the flags given change; a list flag replaces the whole list. " +
			"Status never changes here. A dropped task can't be edited, and neither can a task under a live claim, " +
			"its holder included: release it first. Removing or changing a check, or changing the result, weakens " +
			"the definition of done: it needs --reason, and the task's history records it with --holder as who.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e := graph.Edit{By: holderOf(holder), Reason: reason}
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
			weakened := false
			err = w.Update(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				before, _ := g.ByID(args[0])
				if updated, err = g.UpdateTask(args[0], e, now); err != nil {
					return err
				}
				weakened = len(updated.History) > len(before.History)
				return saveGraph(tx, g)
			})
			if err != nil {
				return err
			}
			text := fmt.Sprintf("updated %s: %s", updated.ID, strings.Join(fields, ", "))
			if weakened {
				text += "\nweakened its definition of done; recorded in its history: " + updated.History[len(updated.History)-1].Reason
			}
			return a.emit(view(updated, now), text)
		},
	}
	f := cmd.Flags()
	f.StringVar(&title, "title", "", "new title")
	f.StringSliceVar(&needs, "needs", []string{}, "new needs, replacing the list (pass --needs= to clear)")
	f.StringSliceVar(&footprint, "footprint", []string{}, "new footprint, replacing the list")
	f.StringVar(&result, "result", "", "new measurable result")
	f.StringArrayVar(&checks, "check", []string{}, "new checks, replacing the list; repeat for several")
	f.StringVar(&reason, "reason", "", "why, when the edit weakens the definition of done (required then)")
	holderFlag(cmd, &holder, "who is making the edit, recorded with a weakening")
	return cmd
}
