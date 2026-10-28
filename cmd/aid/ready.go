package main

import (
	"fmt"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, readyCmd) }

func readyCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "ready",
		Short: "List the frontier: todo tasks whose needs are all done",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, _, err := a.open()
			if err != nil {
				return err
			}
			now := a.now()
			views := []taskView{}
			var text strings.Builder
			err = w.View(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				for _, t := range g.Ready(now) {
					views = append(views, view(t, now))
					fmt.Fprintf(&text, "%s\t%s\n", t.ID, t.Title)
				}
				return nil
			})
			if err != nil {
				return err
			}
			if len(views) == 0 {
				text.WriteString("nothing ready\n")
			}
			return a.emit(views, text.String())
		},
	}
}
