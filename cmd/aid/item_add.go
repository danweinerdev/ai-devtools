package main

import (
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, addItemCmd) }

func addItemCmd(a *app) *cobra.Command {
	var kind, text string
	cmd := &cobra.Command{
		Use:   "add-item",
		Short: "Record an open state item: a note, follow-up, finding or question",
		Long: "Record something noticed that isn't a task but must be followed to the end. " +
			"It stays open until resolve-item says where it went; finish refuses while any item is open.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := oneOf("kind", kind, state.Kinds()); err != nil {
				return err
			}
			w, _, err := a.open()
			if err != nil {
				return err
			}
			var added state.Item
			err = w.Update(func(tx *store.Tx) error {
				r, err := loadState(tx)
				if err != nil {
					return err
				}
				added = state.Item{ID: r.NextID(), Kind: kind, Status: state.Open, Text: text}
				r.Items = append(r.Items, added)
				return saveState(tx, r)
			})
			if err != nil {
				return err
			}
			return a.emit(added, "added "+added.ID)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "one of "+strings.Join(state.Kinds(), ", ")+" (required)")
	cmd.Flags().StringVar(&text, "text", "", "what was noticed, with its evidence (required)")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("text")
	return cmd
}
