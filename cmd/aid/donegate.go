package main

import (
	"fmt"
	"io"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"
)

// doneGate is the definition-of-done gate's judge: it parses `aid <args…>`
// with aid's own command tree and, for an update-task, applies the graph's
// weakening rule to the work's current graph. It only reads.
func doneGate(cwd string, args []string) (task, weakens string, err error) {
	a := &app{cwd: cwd, now: clock(), out: io.Discard, err: io.Discard}
	root := newRoot(a)
	cmd, rest, err := root.Find(args)
	if err != nil || cmd.Name() != "update-task" {
		return "", "", err
	}
	if err := cmd.ParseFlags(rest); err != nil {
		return "", "", err
	}
	f := cmd.Flags()
	if f.NArg() != 1 {
		return "", "", fmt.Errorf("update-task takes one task")
	}
	var e graph.Edit
	if f.Changed("check") {
		checks, _ := f.GetStringArray("check")
		e.Checks = &checks
	}
	if f.Changed("result") {
		result, _ := f.GetString("result")
		e.Result = &result
	}
	if e.Checks == nil && e.Result == nil {
		return "", "", nil
	}
	w, _, err := a.open()
	if err != nil {
		return "", "", err
	}
	id := f.Arg(0)
	var t graph.Task
	err = w.View(func(tx *store.Tx) error {
		g, err := loadGraph(tx)
		if err != nil {
			return err
		}
		var ok bool
		if t, ok = g.ByID(id); !ok {
			return fmt.Errorf("no task %q", id)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return id, graph.WeakeningOf(t, e).String(), nil
}
