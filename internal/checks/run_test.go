package checks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func unixOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a Unix shell")
	}
}

func run(t *testing.T, command string, timeout time.Duration) Result {
	t.Helper()
	return Run(context.Background(), t.TempDir(), command, timeout)
}

func TestPassAndFailByExitCode(t *testing.T) {
	if r := run(t, "exit 0", 0); !r.Passed || r.ExitCode != 0 || r.Err != "" {
		t.Fatalf("pass: %+v", r)
	}
	if r := run(t, "exit 3", 0); r.Passed || r.ExitCode != 3 || r.TimedOut {
		t.Fatalf("fail: %+v", r)
	}
}

func TestCommandNotFound(t *testing.T) {
	unixOnly(t)
	r := run(t, "aid-no-such-command-xyz", 0)
	if r.Passed || r.ExitCode != 127 {
		t.Fatalf("got %+v", r)
	}
}

func TestUnstartableRecordsErr(t *testing.T) {
	r := Run(context.Background(), filepath.Join(t.TempDir(), "missing"), "exit 0", 0)
	if r.Passed || r.Err == "" || r.ExitCode != -1 {
		t.Fatalf("got %+v", r)
	}
}

func TestTimeoutKillsGrandchildrenPromptly(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")
	start := time.Now()
	// The grandchild would write the marker after 1s if it survived the kill.
	r := Run(context.Background(), dir, "(sleep 1; touch survived) & sleep 30 & wait", 200*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Run took %s", elapsed)
	}
	if !r.TimedOut || r.Passed {
		t.Fatalf("got %+v", r)
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a grandchild outlived the timeout")
	}
}

func TestCancelIsNotATimeout(t *testing.T) {
	unixOnly(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	r := Run(ctx, t.TempDir(), "sleep 30", 0)
	if r.Passed || r.TimedOut || r.Err != "cancelled" {
		t.Fatalf("got %+v", r)
	}
}

func TestRunsFromRoot(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	r := Run(context.Background(), dir, "pwd -P", 0)
	want, _ := filepath.EvalSymlinks(dir)
	if strings.TrimSpace(r.Output) != want {
		t.Fatalf("pwd %q want %q", r.Output, want)
	}
}

func TestStdinIsClosed(t *testing.T) {
	unixOnly(t)
	start := time.Now()
	r := run(t, "cat", 5*time.Second)
	if !r.Passed || r.Output != "" || time.Since(start) > 2*time.Second {
		t.Fatalf("cat waited on stdin: %+v", r)
	}
}

func TestCapturesStdoutAndStderr(t *testing.T) {
	unixOnly(t)
	r := run(t, "echo out; echo err >&2", 0)
	if !strings.Contains(r.Output, "out") || !strings.Contains(r.Output, "err") {
		t.Fatalf("output %q", r.Output)
	}
}

func TestOutputKeepsTheTail(t *testing.T) {
	unixOnly(t)
	r := run(t, "i=0; while [ $i -lt 20000 ]; do echo line$i; i=$((i+1)); done; echo LAST", 0)
	if !strings.HasPrefix(r.Output, "[… ") || !strings.Contains(r.Output, "truncated") || !strings.HasSuffix(r.Output, "LAST\n") {
		t.Fatalf("head %q tail %q", r.Output[:40], r.Output[len(r.Output)-20:])
	}
	body := r.Output[strings.Index(r.Output, "\n")+1:]
	if len(body) != outputLimit {
		t.Fatalf("kept %d bytes, want %d", len(body), outputLimit)
	}
}

func TestRunAllContinuesAfterFailure(t *testing.T) {
	rs := RunAll(context.Background(), t.TempDir(), []string{"exit 0", "exit 1", "exit 0"}, 0)
	if len(rs) != 3 || !rs[0].Passed || rs[1].Passed || !rs[2].Passed || rs[1].Command != "exit 1" {
		t.Fatalf("got %+v", rs)
	}
}

func TestPassingCheckLeavesNoProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shell")
	}
	marker := filepath.Join(t.TempDir(), "alive")
	// The background loop writes the marker every 20ms for as long as it
	// lives. The check itself only exits once the loop has written it, so the
	// loop is provably running when the check passes.
	cmd := "(while :; do touch " + marker + "; sleep 0.02; done) >/dev/null 2>&1 & " +
		"i=0; while [ ! -e " + marker + " ] && [ $i -lt 200 ]; do sleep 0.01; i=$((i+1)); done; exit 0"
	if r := Run(context.Background(), t.TempDir(), cmd, 5*time.Second); !r.Passed {
		t.Fatalf("check failed: %+v", r)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the background loop never ran, so this test would prove nothing")
	}
	os.Remove(marker)
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a process the passing check started is still running")
	}
}

func TestExitZeroPassesEvenIfTheContextExpiredAfter(t *testing.T) {
	for _, ctxErr := range []error{context.DeadlineExceeded, context.Canceled} {
		if r := classify(nil, ctxErr); !r.Passed || r.ExitCode != 0 || r.TimedOut || r.Err != "" {
			t.Errorf("exit 0 with ctx %v: got %+v", ctxErr, r)
		}
	}
	failed := errors.New("signal: killed")
	if r := classify(failed, context.DeadlineExceeded); r.Passed || !r.TimedOut {
		t.Errorf("killed by the timeout: got %+v", r)
	}
	if r := classify(failed, context.Canceled); r.Passed || r.TimedOut || r.Err != "cancelled" {
		t.Errorf("killed by a cancel: got %+v", r)
	}
	if r := classify(failed, nil); r.Passed || r.Err != "signal: killed" {
		t.Errorf("failed to run: got %+v", r)
	}
}

func TestOutputTailNeverSplitsARune(t *testing.T) {
	tl := &tail{limit: 10}
	// "é" is two bytes; 7 of them are 14 bytes, so a plain byte cut at 10
	// would start inside the third character.
	tl.Write([]byte(strings.Repeat("é", 7)))
	s := tl.String()
	body := s[strings.Index(s, "\n")+1:]
	if !utf8.ValidString(body) || body != strings.Repeat("é", 5) {
		t.Fatalf("kept %q (%d bytes)", body, len(body))
	}
	// Writes that split a character between them are handled too.
	tl = &tail{limit: 3}
	tl.Write([]byte("aé"[:2]))
	tl.Write([]byte("aé"[2:] + "bc"))
	if body := tl.String()[strings.Index(tl.String(), "\n")+1:]; !utf8.ValidString(body) {
		t.Fatalf("kept invalid UTF-8 %q", body)
	}
}
