package main

import (
	"fmt"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, addTaskCmd) }

func addTaskCmd(a *app) *cobra.Command {
	var t graph.Task
	cmd := &cobra.Command{
		Use:   "add-task",
		Short: "Add a todo task",
		Long: "Add a todo task. Its needs must already exist, so add tasks in dependency order. " +
			"Without --id, a UUIDv7 is generated.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			now := a.now()
			if t.ID == "" {
				id, err := newID(now)
				if err != nil {
					return err
				}
				t.ID = id
			}
			t.State = graph.State{Status: graph.Todo}
			w, _, err := a.open()
			if err != nil {
				return err
			}
			err = w.Update(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				if _, exists := g.ByID(t.ID); exists {
					return fmt.Errorf("task %q already exists; IDs are unique", t.ID)
				}
				g.Tasks = append(g.Tasks, t)
				return saveGraph(tx, g)
			})
			if err != nil {
				return err
			}
			return a.emit(view(t, now), "added "+t.ID)
		},
	}
	f := cmd.Flags()
	f.StringVar(&t.ID, "id", "", "task ID: a short slug (default: a generated UUIDv7)")
	f.StringVar(&t.Title, "title", "", "what it builds, citing the PRD section it serves (required)")
	f.StringSliceVar(&t.Needs, "needs", []string{}, "IDs of the tasks it depends on (comma-separated or repeated)")
	f.StringSliceVar(&t.Footprint, "footprint", []string{}, "files or directories it creates or edits, relative to the repository root (required)")
	f.StringVar(&t.Done.Result, "result", "", "the measurable result that defines done (required)")
	f.StringArrayVar(&t.Done.Checks, "check", []string{}, "a command that proves it, run from the repository root; repeat for several (required)")
	for _, name := range []string{"title", "footprint", "result", "check"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}
