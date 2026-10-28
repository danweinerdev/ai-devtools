package main

import (
	"path/filepath"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/state"
	"github.com/danweinerdev/ai-devtools/internal/store"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, initCmd) }

func initCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "init <name>",
		Short: "Create .aid/<name>/ with an empty task graph and state record",
		Long: "Create .aid/<name>/, named after the PRD the work serves, with an empty task graph and state record. " +
			"Run it at the repository root; an existing .aid/ above the current directory is used instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, ok := findAid(a.cwd)
			if !ok {
				root = a.cwd
			}
			name := args[0]
			_, err := store.Init(filepath.Join(root, AidDir), name, func(tx *store.Tx) error {
				if err := saveGraph(tx, &graph.Graph{Version: graph.Version, Tasks: []graph.Task{}}); err != nil {
					return err
				}
				return saveState(tx, &state.Record{Version: state.Version, Items: []state.Item{}})
			})
			if err != nil {
				return err
			}
			dir := filepath.ToSlash(filepath.Join(AidDir, name))
			return a.emit(map[string]string{"work": name, "dir": dir}, "created "+dir)
		},
	}
}
