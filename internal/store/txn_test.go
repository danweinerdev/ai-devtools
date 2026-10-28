package store

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// counter is a minimal file type with a semantic rule.
type counter struct {
	Version int `json:"version"`
	N       int `json:"n"`
}

func (c *counter) Validate() []string {
	if c.N < 0 {
		return []string{"`n` must not be negative"}
	}
	return nil
}

func seedCounter(n int) func(*Tx) error {
	return func(tx *Tx) error { return tx.Save("counter.json", &counter{Version: 1, N: n}, 1) }
}

func newWork(t *testing.T) Work {
	t.Helper()
	w, err := Init(filepath.Join(t.TempDir(), ".aid"), "Feature", seedCounter(0))
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func increment(w Work) error {
	return w.Update(func(tx *Tx) error {
		var c counter
		if err := tx.Load("counter.json", &c, 1); err != nil {
			return err
		}
		c.N++
		return tx.Save("counter.json", &c, 1)
	})
}

func readN(t *testing.T, w Work) int {
	t.Helper()
	var c counter
	if err := w.View(func(tx *Tx) error { return tx.Load("counter.json", &c, 1) }); err != nil {
		t.Fatal(err)
	}
	return c.N
}

// TestMain doubles as the helper process for the multi-process tests.
func TestMain(m *testing.M) {
	switch os.Getenv("AID_STORE_HELPER") {
	case "":
		os.Exit(m.Run())
	case "hold":
		l, err := acquire(os.Getenv("AID_STORE_DIR"), true, time.Second)
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		fmt.Println("locked")
		_ = l
		time.Sleep(time.Minute)
		os.Exit(0)
	case "increment":
		w := Work{Dir: os.Getenv("AID_STORE_DIR"), WriteTimeout: 30 * time.Second}
		n, _ := strconv.Atoi(os.Getenv("AID_STORE_N"))
		for i := 0; i < n; i++ {
			if err := increment(w); err != nil {
				fmt.Println("error:", err)
				os.Exit(1)
			}
		}
		os.Exit(0)
	}
}

func helper(t *testing.T, mode, dir string, extra ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "AID_STORE_HELPER="+mode, "AID_STORE_DIR="+dir)
	cmd.Env = append(cmd.Env, extra...)
	return cmd
}

func TestLockExclusiveExcludesAndSharedCoexist(t *testing.T) {
	dir := t.TempDir()
	r1, err := acquire(dir, false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := acquire(dir, false, time.Second)
	if err != nil {
		t.Fatal("shared locks must coexist:", err)
	}
	if _, err := acquire(dir, true, 50*time.Millisecond); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("exclusive while shared held: want timeout, got %v", err)
	}
	r1.release()
	r2.release()
	w, err := acquire(dir, true, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquire(dir, false, 50*time.Millisecond); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("shared while exclusive held: want timeout, got %v", err)
	}
	w.release()
	w.release() // idempotent
}

func TestLockKilledHolderReleases(t *testing.T) {
	dir := t.TempDir()
	cmd := helper(t, "hold", dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "locked" {
		t.Fatalf("helper said %q", line)
	}
	if _, err := acquire(dir, true, 50*time.Millisecond); !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("lock not held by the helper: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	l, err := acquire(dir, true, 2*time.Second)
	if err != nil {
		t.Fatal("a killed holder must release the lock:", err)
	}
	l.release()
}

func TestLockUpdateTimesOutWithClearError(t *testing.T) {
	w := newWork(t)
	held, err := acquire(w.Dir, true, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	w.WriteTimeout = 50 * time.Millisecond
	err = increment(w)
	if !errors.Is(err, ErrLockTimeout) || !strings.Contains(err.Error(), "another aid command is holding it") {
		t.Fatalf("got %v", err)
	}
}

func TestTxnMultiProcessWritesSerialise(t *testing.T) {
	w := newWork(t)
	const procs, each = 6, 40
	var wg sync.WaitGroup
	errs := make(chan string, procs)
	for i := 0; i < procs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := helper(t, "increment", w.Dir, "AID_STORE_N="+strconv.Itoa(each))
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Sprintf("%v: %s", err, out)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if got := readN(t, w); got != procs*each {
		t.Fatalf("lost writes: got %d want %d", got, procs*each)
	}
	assertNoTemps(t, w.Dir)
}

func TestTxnValidatesOnLoadAndSave(t *testing.T) {
	w := newWork(t)
	err := w.Update(func(tx *Tx) error { return tx.Save("counter.json", &counter{Version: 1, N: -1}, 1) })
	if err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("save of an invalid value: got %v", err)
	}
	if readN(t, w) != 0 {
		t.Fatal("an invalid save was written")
	}
	os.WriteFile(filepath.Join(w.Dir, "counter.json"), []byte(`{"version": 1, "n": -5}`), 0o644)
	err = w.View(func(tx *Tx) error { var c counter; return tx.Load("counter.json", &c, 1) })
	if err == nil || !strings.Contains(err.Error(), "counter.json is invalid") || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("load of an invalid file: got %v", err)
	}
	err = w.View(func(tx *Tx) error { var c counter; return tx.Load("state.json", &c, 1) })
	if err == nil || !strings.Contains(err.Error(), "state.json is invalid") {
		t.Fatalf("load of a missing file: got %v", err)
	}
}

func TestTxnOutsideEditRetriesFromFreshRead(t *testing.T) {
	w := newWork(t)
	path := filepath.Join(w.Dir, "counter.json")
	attempts := 0
	err := w.Update(func(tx *Tx) error {
		attempts++
		var c counter
		if err := tx.Load("counter.json", &c, 1); err != nil {
			return err
		}
		if attempts == 1 { // an editor ignoring the lock writes mid-transaction
			os.WriteFile(path, []byte(`{"version": 1, "n": 10}`), 0o644)
		}
		c.N++
		return tx.Save("counter.json", &c, 1)
	})
	if err != nil || attempts != 2 {
		t.Fatalf("err %v after %d attempts", err, attempts)
	}
	if got := readN(t, w); got != 11 {
		t.Fatalf("retry didn't start from the fresh read: n=%d", got)
	}
}

func TestTxnPersistentOutsideEditsFailWithoutPartialWrites(t *testing.T) {
	w := newWork(t)
	other := filepath.Join(w.Dir, "other.json")
	os.WriteFile(other, []byte(`{"version": 1, "n": 0}`), 0o644)
	attempts := 0
	err := w.Update(func(tx *Tx) error {
		attempts++
		var a, b counter
		if err := tx.Load("counter.json", &a, 1); err != nil {
			return err
		}
		if err := tx.Load("other.json", &b, 1); err != nil {
			return err
		}
		os.WriteFile(other, []byte(fmt.Sprintf(`{"version": 1, "n": %d}`, 100+attempts)), 0o644)
		a.N, b.N = 1, 1
		if err := tx.Save("counter.json", &a, 1); err != nil {
			return err
		}
		return tx.Save("other.json", &b, 1)
	})
	var cw *ErrConcurrentWrite
	if !errors.As(err, &cw) || cw.File != "other.json" || attempts != updateAttempts {
		t.Fatalf("got %v after %d attempts", err, attempts)
	}
	if readN(t, w) != 0 {
		t.Fatal("counter.json was written although other.json conflicted")
	}
}

func TestTxnUpdateSweepsTemps(t *testing.T) {
	w := newWork(t)
	stray := filepath.Join(w.Dir, ".counter.json"+tempMarker+"dead")
	os.WriteFile(stray, []byte("partial"), 0o644)
	if err := increment(w); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatal("abandoned temp file not swept")
	}
}

func TestTxnViewIsReadOnly(t *testing.T) {
	w := newWork(t)
	err := w.View(func(tx *Tx) error { return tx.Save("counter.json", &counter{Version: 1}, 1) })
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("got %v", err)
	}
}

func TestTxnInit(t *testing.T) {
	aid := filepath.Join(t.TempDir(), ".aid")
	if _, err := Init(aid, "../escape", seedCounter(0)); err == nil {
		t.Fatal("a path-like name was accepted")
	}
	if _, err := Init(aid, "Broken", func(*Tx) error { return errors.New("seed failed") }); err == nil {
		t.Fatal("seed error swallowed")
	}
	if _, err := os.Stat(filepath.Join(aid, "Broken")); !os.IsNotExist(err) {
		t.Fatal("a failed init left a work directory")
	}

	const racers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, exists := 0, 0
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Init(aid, "Feature", seedCounter(i))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, ErrExists):
				exists++
			default:
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if won != 1 || exists != racers-1 {
		t.Fatalf("racing inits: %d won, %d saw it exists", won, exists)
	}
	assertNoTemps(t, aid)
	if got, _ := os.ReadFile(filepath.Join(aid, ".gitignore")); string(got) != GitIgnore {
		t.Fatalf(".gitignore: %q", got)
	}
	if _, err := Open(aid, "Missing"); err == nil {
		t.Fatal("opened a missing work")
	}
}

func TestTxnOpenRefusesNamesOutsideAidDir(t *testing.T) {
	root := t.TempDir()
	aid := filepath.Join(root, ".aid")
	if _, err := Init(aid, "Feature", seedCounter(0)); err != nil {
		t.Fatal(err)
	}
	// A directory beside .aid/ that looks like a work must stay unreachable.
	os.MkdirAll(filepath.Join(root, "outside"), 0o755)
	for _, name := range []string{"../outside", ".", "..", "Feature/../Feature", "a/b", ""} {
		if _, err := Open(aid, name); err == nil || !strings.Contains(err.Error(), "work name") {
			t.Errorf("Open(%q): want a work-name refusal, got %v", name, err)
		}
	}
	if _, err := Open(aid, "Feature"); err != nil {
		t.Fatal(err)
	}
}

func TestTxnInitResumesAfterACrashBetweenFiles(t *testing.T) {
	aid := filepath.Join(t.TempDir(), ".aid")
	two := func(tx *Tx) error {
		if err := tx.Save("counter.json", &counter{Version: 1, N: 1}, 1); err != nil {
			return err
		}
		return tx.Save("other.json", &counter{Version: 1, N: 2}, 1)
	}
	// Simulate a crash after the first file: the directory holds only counter.json.
	dir := filepath.Join(aid, "Feature")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "counter.json"), []byte(`{"version": 1, "n": 7}`), 0o644)

	w, err := Init(aid, "Feature", two)
	if err != nil {
		t.Fatalf("init must resume a partial work: %v", err)
	}
	if got := readN(t, w); got != 7 {
		t.Fatalf("resume overwrote the file already there: n=%d", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "other.json")); err != nil {
		t.Fatal("resume didn't write the missing file")
	}
	if _, err := Init(aid, "Feature", two); !errors.Is(err, ErrExists) {
		t.Fatalf("a complete work must refuse init, got %v", err)
	}
}

func TestTxnInitFailedSeedLeavesNoDirectory(t *testing.T) {
	aid := filepath.Join(t.TempDir(), ".aid")
	if _, err := Init(aid, "Broken", func(*Tx) error { return errors.New("seed failed") }); err == nil {
		t.Fatal("seed error swallowed")
	}
	if _, err := os.Stat(filepath.Join(aid, "Broken")); !os.IsNotExist(err) {
		t.Fatal("a failed init left its directory behind")
	}
	// A directory that existed before (holding the breakdown) is kept.
	dir := filepath.Join(aid, "Planned")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "breakdown.md"), []byte("# plan\n"), 0o644)
	Init(aid, "Planned", func(*Tx) error { return errors.New("seed failed") })
	if _, err := os.Stat(filepath.Join(dir, "breakdown.md")); err != nil {
		t.Fatal("a failed init removed a directory it didn't create")
	}
}

func TestTxnInitSweepsOnlyStaleTopLevelTemps(t *testing.T) {
	aid := filepath.Join(t.TempDir(), ".aid")
	os.MkdirAll(aid, 0o755)
	stale := filepath.Join(aid, ".gitignore"+tempMarker+"old")
	fresh := filepath.Join(aid, ".gitignore"+tempMarker+"new")
	os.WriteFile(stale, []byte("x"), 0o644)
	os.WriteFile(fresh, []byte("x"), 0o644)
	old := time.Now().Add(-staleTemp - time.Minute)
	os.Chtimes(stale, old, old)
	if _, err := Init(aid, "Feature", seedCounter(0)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a stale temp file under .aid/ wasn't swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh temp file (a concurrent init's) was swept")
	}
}

func TestTxnGitIgnoreCoversLockAndTemps(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	w, err := Init(filepath.Join(repo, ".aid"), "Feature", seedCounter(0))
	if err != nil {
		t.Fatal(err)
	}
	if err := increment(w); err != nil { // creates the lock file
		t.Fatal(err)
	}
	ignored := func(rel string) bool {
		return exec.Command("git", "-C", repo, "check-ignore", "-q", rel).Run() == nil
	}
	for _, rel := range []string{".aid/Feature/.lock", ".aid/Feature/.counter.json" + tempMarker + "x", ".aid/.gitignore" + tempMarker + "x"} {
		if !ignored(rel) {
			t.Errorf("%s should be ignored", rel)
		}
	}
	for _, rel := range []string{".aid/Feature/counter.json", ".aid/Feature/breakdown.md", ".aid/.gitignore"} {
		if ignored(rel) {
			t.Errorf("%s must be committed, but is ignored", rel)
		}
	}
}

func TestTxnInitIntoExistingDirectory(t *testing.T) {
	aid := filepath.Join(t.TempDir(), ".aid")
	dir := filepath.Join(aid, "Feature")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "breakdown.md"), []byte("# plan\n"), 0o644)

	const racers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, exists := 0, 0
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Init(aid, "Feature", seedCounter(i))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, ErrExists):
				exists++
			default:
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if won != 1 || exists != racers-1 {
		t.Fatalf("racing in-place inits: %d won, %d saw it exists", won, exists)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "breakdown.md")); string(got) != "# plan\n" {
		t.Fatal("init touched the breakdown")
	}
	if got, _ := os.ReadFile(filepath.Join(aid, ".gitignore")); string(got) != GitIgnore {
		t.Fatal(".gitignore not written")
	}
	w, _ := Open(aid, "Feature")
	if err := increment(w); err != nil {
		t.Fatal(err)
	}
}
