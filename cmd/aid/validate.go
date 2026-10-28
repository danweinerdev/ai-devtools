package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, validateCmd) }

// problem is one thing wrong, located by file.
type problem struct {
	File    string `json:"file"`
	Problem string `json:"problem"`
}

func validateCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check tasks.json and state.json without writing, reporting every problem",
		Long: "Check both files the way every command does on load, plus the checks across them, and " +
			"report every problem in both rather than stopping at the first. Writes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, _, err := a.open()
			if err != nil {
				return err
			}
			var problems []problem
			var g graph.Graph
			var r state.Record
			graphOK, stateOK := false, false
			err = w.View(func(tx *store.Tx) error {
				var err error
				if problems, graphOK, err = collect(problems, graph.File, tx.Load(graph.File, &g, graph.Version)); err != nil {
					return err
				}
				problems, stateOK, err = collect(problems, state.File, tx.Load(state.File, &r, state.Version))
				return err
			})
			if err != nil {
				return err
			}
			if graphOK && stateOK {
				for _, it := range r.Items {
					if it.Status == state.Resolved && it.Resolution.As == state.AsTask {
						if _, ok := g.ByID(it.Resolution.Where); !ok {
							problems = append(problems, problem{state.File,
								fmt.Sprintf("item %s is resolved as task %q, which doesn't exist in %s", it.ID, it.Resolution.Where, graph.File)})
						}
					}
				}
			}
			if len(problems) > 0 {
				lines := make([]string, 0, len(problems))
				for _, p := range problems {
					lines = append(lines, "  "+p.File+": "+p.Problem)
				}
				return &refusal{
					msg:    fmt.Sprintf("invalid: %d problem(s)\n%s", len(problems), strings.Join(lines, "\n")),
					detail: map[string]any{"problems": problems},
				}
			}
			return a.emit(map[string]any{"valid": true, "taskCount": len(g.Tasks), "itemCount": len(r.Items)},
				fmt.Sprintf("valid: %d tasks, %d items", len(g.Tasks), len(r.Items)))
		},
	}
}

// collect turns a load error into problems for file. A decode or validation
// failure is a list of problems; anything else (an unreadable file) is a
// genuine error and stops validation.
func collect(problems []problem, file string, err error) ([]problem, bool, error) {
	if err == nil {
		return problems, true, nil
	}
	var de *store.DecodeError
	if !errors.As(err, &de) {
		return problems, false, err
	}
	for _, p := range de.Problems {
		problems = append(problems, problem{File: file, Problem: p})
	}
	return problems, false, nil
}
