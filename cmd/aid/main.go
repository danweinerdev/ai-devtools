// Command aid holds the task graph and state record of a piece of Make work:
// `.aid/<name>/tasks.json` and `.aid/<name>/state.json`. Every command
// validates both files on load, every write is locked and atomic, and aid
// never calls git. See skills/aid-graph and skills/aid-state for usage.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// An interrupt or termination cancels the context, which stops running
	// checks and kills their process trees; otherwise they'd outlive aid.
	// stop() restores default handling, so a second signal kills at once.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	a, err := defaultApp()
	if err != nil {
		stop()
		fmt.Fprintln(os.Stderr, "aid: "+err.Error())
		os.Exit(1)
	}
	code := run(ctx, a, os.Args[1:])
	stop()
	os.Exit(code)
}

// run executes one command line and returns the exit code. Errors go to
// stderr; with --json they also go to stdout as {"error", "detail"} so an
// agent can act on a refusal without parsing prose.
func run(ctx context.Context, a *app, args []string) int {
	root := newRoot(a)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	fmt.Fprintln(a.err, "aid: "+err.Error())
	if a.json {
		out := map[string]any{"error": err.Error()}
		if r, ok := asRefusal(err); ok && r.detail != nil {
			out["detail"] = r.detail
		}
		_ = a.emit(out, "")
	}
	return 1
}
