package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/checks"
	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, checkCmd) }

// taskCheck is one task's outcome in a batch.
type taskCheck struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Passed bool        `json:"passed"`
	Checks []checkView `json:"checks"`
}

// batchResult is the whole batch, as --json prints it.
type batchResult struct {
	Tasks  []taskCheck `json:"tasks"`
	Passed int         `json:"passed"`
	Failed int         `json:"failed"`
	// Uncovered lists the --paths no task's footprint covers.
	Uncovered []string `json:"uncovered,omitempty"`
}

func checkCmd(a *app) *cobra.Command {
	var paths []string
	var strict bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Run the checks of every task, or of those whose footprint covers --paths",
		Long: "Run the checks of every task that isn't dropped, or with --paths only those whose footprint " +
			"covers a given file or directory, and report pass or fail per task. Checks run from the " +
			"repository root without holding the lock. Nothing is written: re-run the batch whenever in doubt. " +
			"A path no task covers is listed but isn't a failure, since unowned files (often docs) need no " +
			"re-proving; --strict makes any uncovered path fail the run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w, root, err := a.open()
			if err != nil {
				return err
			}
			if strict && !cmd.Flags().Changed("paths") {
				return fmt.Errorf("--strict needs --paths: it fails on paths no task covers")
			}
			rel, err := rootRelative(root, a.cwd, paths)
			if err != nil {
				return err
			}
			now := a.now()
			var selected []graph.Task
			var uncovered []string
			err = w.View(func(tx *store.Tx) error {
				g, err := loadGraph(tx)
				if err != nil {
					return err
				}
				if cmd.Flags().Changed("paths") {
					selected = g.Touching(rel)
					uncovered = uncoveredPaths(g, rel)
					return nil
				}
				for _, t := range g.Tasks {
					if t.State.Status != graph.Dropped {
						selected = append(selected, t)
					}
				}
				return nil
			})
			if err != nil {
				return err
			}

			res := batchResult{Tasks: []taskCheck{}, Uncovered: uncovered}
			var text strings.Builder
			if len(uncovered) > 0 {
				fmt.Fprintf(&text, "no task covers: %s\n", strings.Join(uncovered, ", "))
			}
			if len(selected) == 0 {
				text.WriteString("no tasks selected; nothing to check")
				if strict && len(uncovered) > 0 {
					return &refusal{msg: "--strict: " + text.String(), detail: res}
				}
				return a.emit(res, text.String())
			}
			for _, t := range selected {
				views, passed := checkViews(checks.RunAll(cmd.Context(), root, t.Done.Checks, 0))
				status := t.Effective(now)
				res.Tasks = append(res.Tasks, taskCheck{ID: t.ID, Status: status, Passed: passed, Checks: views})
				mark := "pass"
				if passed {
					res.Passed++
				} else {
					res.Failed++
					mark = "FAIL"
				}
				fmt.Fprintf(&text, "%s  %s (%s)\n%s", mark, t.ID, status, checkText(views))
			}
			fmt.Fprintf(&text, "%d passed, %d failed", res.Passed, res.Failed)
			if res.Failed > 0 {
				return &refusal{msg: "the batch failed\n" + text.String(), detail: res}
			}
			if strict && len(uncovered) > 0 {
				return &refusal{msg: "--strict: some paths have no task\n" + text.String(), detail: res}
			}
			return a.emit(res, text.String())
		},
	}
	cmd.Flags().StringSliceVar(&paths, "paths", nil,
		"files or directories a change touched (comma-separated or repeated); absolute or relative to the current directory")
	cmd.Flags().BoolVar(&strict, "strict", false, "with --paths, fail when any given path is covered by no task")
	return cmd
}

// uncoveredPaths are the paths no live task's footprint covers, in the order given.
func uncoveredPaths(g *graph.Graph, paths []string) []string {
	var out []string
	for _, p := range paths {
		covered := false
		for _, t := range g.Tasks {
			if t.State.Status == graph.Dropped {
				continue
			}
			for _, entry := range t.Footprint {
				if graph.Covers(entry, p) {
					covered = true
				}
			}
		}
		if !covered {
			out = append(out, p)
		}
	}
	return out
}

// rootRelative converts paths, absolute or relative to cwd, into clean slash
// paths relative to the repository root, refusing any outside it.
func rootRelative(root, cwd string, paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(cwd, p)
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("--paths %q is outside the repository root %s", p, root)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out, nil
}
