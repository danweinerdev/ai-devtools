package main

import (
	"errors"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, updateItemCmd) }

func updateItemCmd(a *app) *cobra.Command {
	var kind, text string
	cmd := &cobra.Command{
		Use:   "update-item <id>",
		Short: "Edit an item's kind or text",
		Long:  "Edit an item's kind or text; only the flags given change. Resolved items can still be edited.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			setKind, setText := cmd.Flags().Changed("kind"), cmd.Flags().Changed("text")
			if !setKind && !setText {
				return errors.New("nothing to change; pass --kind and/or --text")
			}
			if setKind {
				if err := oneOf("kind", kind, state.Kinds()); err != nil {
					return err
				}
			}
			w, _, err := a.open()
			if err != nil {
				return err
			}
			var updated state.Item
			err = w.Update(func(tx *store.Tx) error {
				r, err := loadState(tx)
				if err != nil {
					return err
				}
				i, err := itemIndex(r, args[0])
				if err != nil {
					return err
				}
				if setKind {
					r.Items[i].Kind = kind
				}
				if setText {
					r.Items[i].Text = text
				}
				updated = r.Items[i]
				return saveState(tx, r)
			})
			if err != nil {
				return err
			}
			return a.emit(updated, "updated "+updated.ID)
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "one of "+strings.Join(state.Kinds(), ", "))
	cmd.Flags().StringVar(&text, "text", "", "the new text")
	return cmd
}
