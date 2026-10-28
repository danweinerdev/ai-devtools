// Package checks runs a task's checks: non-interactive commands run from the
// repository root that pass or fail by exit code.
package checks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// DefaultTimeout bounds a check when the caller gives no timeout.
	DefaultTimeout = 10 * time.Minute
	// outputLimit is how much output a Result keeps: the tail, where a
	// failing test run puts its summary.
	outputLimit = 64 << 10
	// waitDelay bounds the wait for output pipes after the process is gone,
	// in case something escaped the kill and still holds them.
	waitDelay = 5 * time.Second
)

// Result is one check's outcome.
type Result struct {
	Command  string
	Passed   bool
	ExitCode int // -1 when the command didn't exit normally
	TimedOut bool
	Output   string // combined stdout and stderr, last 64 KiB
	Duration time.Duration
	Err      string // set when the command couldn't be run at all
}

// Run runs command through the platform shell with root as the working
// directory, stdin from the null device, and the inherited environment. On
// timeout or cancellation the whole process tree is killed, so a check that
// spawned a server can't outlive it. Zero timeout means DefaultTimeout.
func Run(ctx context.Context, root, command string, timeout time.Duration) Result {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell[0], append(shell[1:], command)...)
	cmd.Dir = root
	cmd.Stdin = nil // exec connects a nil Stdin to the null device
	out := &tail{limit: outputLimit}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	procs := newTree(cmd)
	defer procs.close()
	cmd.Cancel = func() error { return procs.kill(cmd) }

	start := time.Now()
	err := cmd.Start()
	if err == nil {
		procs.started(cmd)
		err = cmd.Wait()
	}
	res := classify(err, ctx.Err())
	res.Command, res.Duration, res.Output = command, time.Since(start), out.String()
	return res
}

// classify turns the process result into a Result. The process decides first:
// a command that exited 0 passed, even if the context expired a moment after.
// Only a failed run is blamed on the timeout or cancellation, when there was
// one, since the kill is what made it fail.
func classify(err, ctxErr error) Result {
	res := Result{ExitCode: -1}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.Passed, res.ExitCode = true, 0
	case ctxErr != nil:
		res.TimedOut = errors.Is(ctxErr, context.DeadlineExceeded)
		if !res.TimedOut {
			res.Err = "cancelled"
		}
	case errors.As(err, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		res.Err = err.Error()
	}
	return res
}

// RunAll runs every command in order, continuing after failures so the
// report is complete.
func RunAll(ctx context.Context, root string, commands []string, timeout time.Duration) []Result {
	results := make([]Result, 0, len(commands))
	for _, c := range commands {
		results = append(results, Run(ctx, root, c, timeout))
	}
	return results
}

// tail keeps the last limit bytes written to it. Stdout and stderr share it.
type tail struct {
	mu      sync.Mutex
	limit   int
	buf     []byte
	dropped int64
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		// Never start the kept tail mid-character: skip to the next rune
		// start, keeping slightly less than the limit.
		for over < len(t.buf) && !utf8.RuneStart(t.buf[over]) {
			over++
		}
		t.dropped += int64(over)
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dropped == 0 {
		return string(t.buf)
	}
	return fmt.Sprintf("[… %d bytes of earlier output truncated …]\n", t.dropped) + string(t.buf)
}
