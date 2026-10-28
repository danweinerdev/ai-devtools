package main

import (
	"fmt"
	"time"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, claimCmd) }

// DefaultLease is how long a claim lasts unless --lease says otherwise.
const DefaultLease = 2 * time.Hour

func claimCmd(a *app) *cobra.Command {
	var holder string
	var lease time.Duration
	cmd := &cobra.Command{
		Use:   "claim-task <id>",
		Short: "Mark a ready task in-progress, held by --holder for --lease",
		Long: "Mark a ready task in-progress, held by --holder until the lease lapses. One holder at a time; " +
			"a lapsed lease releases itself, and the same holder claiming again renews it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w, _, err := a.open()
			if err != nil {
				return err
			}
			holder := holderOf(holder)
			now := a.now()
			var claimed graph.Task
			err = w.Update(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				if claimed, err = g.Claim(args[0], holder, lease, now); err != nil {
					return err
				}
				return saveGraph(tx, g)
			})
			if err != nil {
				return err
			}
			return a.emit(view(claimed, now), fmt.Sprintf("claimed %s for %s until %s",
				claimed.ID, holder, claimed.State.Claim.Expires.Format(time.RFC3339)))
		},
	}
	holderFlag(cmd, &holder, "who is doing it, e.g. the role")
	cmd.Flags().DurationVar(&lease, "lease", DefaultLease, "how long the claim lasts")
	return cmd
}
