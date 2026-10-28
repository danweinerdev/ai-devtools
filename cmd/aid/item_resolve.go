package main

import (
	"fmt"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, resolveItemCmd) }

func resolveItemCmd(a *app) *cobra.Command {
	var as, where string
	cmd := &cobra.Command{
		Use:   "resolve-item <id>",
		Short: "Close an item, recording where it went",
		Long: "Close an open item, recording its durable home: done (handled; say where), task (an " +
			"existing task ID in the graph), prd (proposed as a fragment), or guidance (proposed to " +
			"the project's agent guidance or a skill).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf("as", as, state.Resolutions()); err != nil {
				return err
			}
			if strings.TrimSpace(where) == "" {
				return fmt.Errorf("--where is required: say where it went")
			}
			w, _, err := a.open()
			if err != nil {
				return err
			}
			var resolved state.Item
			err = w.Update(func(tx *store.Tx) error {
				r, err := loadState(tx)
				if err != nil {
					return err
				}
				i, err := itemIndex(r, args[0])
				if err != nil {
					return err
				}
				it := &r.Items[i]
				if it.Status == state.Resolved {
					return fmt.Errorf("item %q is already resolved (%s: %s)", it.ID, it.Resolution.As, it.Resolution.Where)
				}
				if as == state.AsTask {
					g, err := loadGraph(tx)
					if err != nil {
						return err
					}
					if _, ok := g.ByID(where); !ok {
						return fmt.Errorf("--as task needs --where to name a task in the graph; no task %q (add it first)", where)
					}
				}
				it.Status = state.Resolved
				it.Resolution = state.Resolution{As: as, Where: where}
				resolved = *it
				return saveState(tx, r)
			})
			if err != nil {
				return err
			}
			return a.emit(resolved, fmt.Sprintf("resolved %s → %s: %s", resolved.ID, as, where))
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "where it went: "+strings.Join(state.Resolutions(), ", ")+" (required)")
	cmd.Flags().StringVar(&where, "where", "", "the specifics: a task ID for --as task, otherwise a short description (required)")
	_ = cmd.MarkFlagRequired("as")
	_ = cmd.MarkFlagRequired("where")
	return cmd
}
