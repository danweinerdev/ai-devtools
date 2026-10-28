package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, itemsCmd) }

// loadState is the one way commands read the state record: strict decode plus
// full validation, every time.
func loadState(tx *store.Tx) (*state.Record, error) {
	var r state.Record
	if err := tx.Load(state.File, &r, state.Version); err != nil {
		return nil, err
	}
	return &r, nil
}

func saveState(tx *store.Tx, r *state.Record) error {
	return tx.Save(state.File, r, state.Version)
}

// itemIndex returns the position of id in r, or an error.
func itemIndex(r *state.Record, id string) (int, error) {
	if i := r.ByID(id); i >= 0 {
		return i, nil
	}
	return -1, fmt.Errorf("no item %q (%d items; `aid items` lists them)", id, len(r.Items))
}

// oneOf refuses a flag value outside allowed.
func oneOf(flag, value string, allowed []string) error {
	if !slices.Contains(allowed, value) {
		return fmt.Errorf("--%s %q must be one of %s", flag, value, strings.Join(allowed, ", "))
	}
	return nil
}

func itemLine(it state.Item) string {
	line := fmt.Sprintf("%s\t%s\t%s\t%s", it.ID, it.Kind, it.Status, it.Text)
	if it.Status == state.Resolved {
		line += fmt.Sprintf("\t→ %s: %s", it.Resolution.As, it.Resolution.Where)
	}
	return line
}

func itemsCmd(a *app) *cobra.Command {
	var kind, status string
	cmd := &cobra.Command{
		Use:   "items",
		Short: "List state items, optionally filtered by kind and status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if kind != "" {
				if err := oneOf("kind", kind, state.Kinds()); err != nil {
					return err
				}
			}
			if status != "" {
				if err := oneOf("status", status, state.Statuses()); err != nil {
					return err
				}
			}
			w, _, err := a.open()
			if err != nil {
				return err
			}
			items := []state.Item{}
			var text strings.Builder
			err = w.View(func(tx *store.Tx) error {
				r, err := loadState(tx)
				if err != nil {
					return err
				}
				for _, it := range r.Items {
					if (kind == "" || it.Kind == kind) && (status == "" || it.Status == status) {
						items = append(items, it)
						text.WriteString(itemLine(it) + "\n")
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			if len(items) == 0 {
				text.WriteString("no items\n")
			}
			return a.emit(items, text.String())
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "only items of this kind: "+strings.Join(state.Kinds(), ", "))
	cmd.Flags().StringVar(&status, "status", "", "only items with this status: "+strings.Join(state.Statuses(), ", "))
	return cmd
}
