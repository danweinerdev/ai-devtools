package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Validator is implemented by file types with rules beyond their shape (unique
// IDs, no cycles…). Tx calls it after every decode and before every encode, so
// an invalid file is never accepted and never written.
type Validator interface {
	Validate() []string
}

// GitIgnore is `.aid/.gitignore`: the per-work lock and crashed writers' temp
// files, at any depth. Everything else under `.aid/` is committed.
const GitIgnore = `# Written by aid. Lock files and crash leftovers; the rest of .aid/ is committed.
*/` + LockFile + `
.*` + tempMarker + `*
`

// staleTemp is how old a temp file directly under `.aid/` must be before Init
// sweeps it: a younger one may belong to a concurrent init's .gitignore write.
const staleTemp = 10 * time.Minute

var workName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Work is one piece of work: the directory `.aid/<name>/`.
type Work struct {
	Dir string
	// WriteTimeout bounds the wait for the write lock; zero means 10 seconds.
	WriteTimeout time.Duration
}

// ErrExists reports that init found the work directory already there.
var ErrExists = errors.New("already exists")

// ErrConcurrentWrite reports that a file changed under the lock while a
// transaction ran, which only a writer that ignores the lock (an editor, say)
// can do. Update retries; the error surfaces only if it keeps happening.
type ErrConcurrentWrite struct{ File string }

func (e *ErrConcurrentWrite) Error() string {
	return fmt.Sprintf("%s was changed by something that doesn't take aid's lock; edit .aid files only through aid", e.File)
}

const updateAttempts = 3

// Init creates `<aidDir>/<name>/` and writes the files seed saves, under the
// work's lock, so of several racing inits exactly one succeeds.
//
// The directory may already exist holding other files (the breakdown is
// written there before init). If every seed file is already there, the work is
// initialised and Init refuses with ErrExists. If only some are (an init that
// crashed between files), Init resumes: it writes the missing ones and leaves
// the present ones alone.
func Init(aidDir, name string, seed func(*Tx) error) (Work, error) {
	if !workName.MatchString(name) {
		return Work{}, fmt.Errorf("work name %q: use letters, digits, '.', '_' or '-', starting with a letter or digit", name)
	}
	work := Work{Dir: filepath.Join(aidDir, name)}
	if err := os.MkdirAll(aidDir, 0o755); err != nil {
		return Work{}, err
	}
	if _, err := sweepTemps(aidDir, staleTemp); err != nil {
		return Work{}, fmt.Errorf("sweeping temp files in %s: %w", aidDir, err)
	}
	if err := ensureFile(filepath.Join(aidDir, ".gitignore"), GitIgnore); err != nil {
		return Work{}, err
	}
	created := false
	switch fi, err := os.Stat(work.Dir); {
	case err == nil && !fi.IsDir():
		return Work{}, fmt.Errorf("%s exists and isn't a directory", work.Dir)
	case os.IsNotExist(err):
		created = true
	case err != nil:
		return Work{}, err
	}
	if err := os.MkdirAll(work.Dir, 0o755); err != nil {
		return Work{}, err
	}
	if err := seedUnderLock(work, seed); err != nil {
		if created {
			removeIfOnlyLock(work.Dir)
		}
		return Work{}, err
	}
	return work, syncDir(aidDir)
}

// seedUnderLock runs seed under the work lock and writes the seed files that
// are missing; all present means the work is already initialised.
func seedUnderLock(work Work, seed func(*Tx) error) error {
	lock, err := acquire(work.Dir, true, writeLockTimeout)
	if err != nil {
		return fmt.Errorf("%s: %w", work.Dir, err)
	}
	defer lock.release()
	tx := &Tx{dir: work.Dir, writable: true, digests: map[string]string{}, staged: map[string][]byte{}}
	if err := seed(tx); err != nil {
		return err
	}
	var missing []string
	for _, file := range tx.order {
		if _, err := os.Stat(filepath.Join(work.Dir, file)); os.IsNotExist(err) {
			missing = append(missing, file)
		} else if err != nil {
			return err
		}
	}
	if len(missing) == 0 {
		return fmt.Errorf("%s: %w", work.Dir, ErrExists)
	}
	for _, file := range missing {
		if err := WriteAtomic(filepath.Join(work.Dir, file), tx.staged[file]); err != nil {
			return err
		}
	}
	return nil
}

// removeIfOnlyLock removes a directory Init created when a failed seed left
// nothing in it but the lock, so a failed init leaves no trace.
func removeIfOnlyLock(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != LockFile {
			return
		}
	}
	_ = os.Remove(filepath.Join(dir, LockFile))
	_ = os.Remove(dir)
}

// ensureFile writes content to path unless it's already exactly that.
func ensureFile(path, content string) error {
	if cur, err := os.ReadFile(path); err == nil && string(cur) == content {
		return nil
	}
	return WriteAtomic(path, []byte(content))
}

// Open returns the work at `<aidDir>/<name>/`, which must exist. The name
// follows Init's rule, so it can't reach outside aidDir.
func Open(aidDir, name string) (Work, error) {
	if !workName.MatchString(name) {
		return Work{}, fmt.Errorf("work name %q: use letters, digits, '.', '_' or '-', starting with a letter or digit", name)
	}
	dir := filepath.Join(aidDir, name)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return Work{}, fmt.Errorf("no work %q in %s (run `aid init` first)", name, aidDir)
	}
	return Work{Dir: dir}, nil
}

// Update runs fn under the exclusive lock and then writes what it saved. A
// file changed under the lock by a non-cooperating writer makes fn run again
// from a fresh read; nothing is written unless every file it read is
// unchanged.
func (w Work) Update(fn func(*Tx) error) error {
	timeout := w.WriteTimeout
	if timeout == 0 {
		timeout = writeLockTimeout
	}
	lock, err := acquire(w.Dir, true, timeout)
	if errors.Is(err, ErrLockTimeout) {
		return fmt.Errorf("%s: %w after %s; another aid command is holding it", w.Dir, err, timeout)
	}
	if err != nil {
		return err
	}
	defer lock.release()
	if _, err := SweepTemps(w.Dir); err != nil {
		return fmt.Errorf("sweeping temp files in %s: %w", w.Dir, err)
	}
	for attempt := 1; ; attempt++ {
		tx := &Tx{dir: w.Dir, writable: true, digests: map[string]string{}, staged: map[string][]byte{}}
		if err := fn(tx); err != nil {
			return err
		}
		err := tx.commit()
		var cw *ErrConcurrentWrite
		if !errors.As(err, &cw) || attempt == updateAttempts {
			return err
		}
	}
}

// View runs fn under a shared lock, read-only. If the lock can't be had within
// a couple of seconds it reads anyway: atomic writes leave only whole files.
func (w Work) View(fn func(*Tx) error) error {
	lock, _ := acquire(w.Dir, false, readLockWindow)
	defer lock.release()
	return fn(&Tx{dir: w.Dir, digests: map[string]string{}})
}

// Tx is one transaction's view of a work directory.
type Tx struct {
	dir      string
	writable bool
	digests  map[string]string // file -> digest when loaded ("" if absent)
	staged   map[string][]byte
	order    []string
}

// Load strictly decodes file into v and validates it.
func (tx *Tx) Load(file string, v any, version int) error {
	raw, err := os.ReadFile(filepath.Join(tx.dir, file))
	if os.IsNotExist(err) {
		return &DecodeError{File: file, Problems: []string{"missing; the work directory is incomplete"}}
	}
	if err != nil {
		return err
	}
	tx.digests[file] = digest(raw)
	if err := Decode(file, raw, v, version); err != nil {
		return err
	}
	if val, ok := v.(Validator); ok {
		if problems := val.Validate(); len(problems) > 0 {
			return &DecodeError{File: file, Problems: problems}
		}
	}
	return nil
}

// Save validates and encodes v and stages it; it's written when the
// transaction commits.
func (tx *Tx) Save(file string, v any, version int) error {
	if !tx.writable {
		return fmt.Errorf("aid bug: saving %s in a read-only transaction", file)
	}
	if val, ok := v.(Validator); ok {
		if problems := val.Validate(); len(problems) > 0 {
			return &DecodeError{File: file, Problems: problems}
		}
	}
	data, err := Encode(file, v, version)
	if err != nil {
		return err
	}
	if _, seen := tx.staged[file]; !seen {
		tx.order = append(tx.order, file)
	}
	tx.staged[file] = data
	return nil
}

// commit checks every loaded file is unchanged, then writes the staged files.
// Checking all before writing any means a conflict never leaves a mix. Each
// write is atomic on its own, but not across files: a crash between two
// files' renames leaves the first new and the second old. No command saves
// more than one file today; one that does must tolerate that.
func (tx *Tx) commit() error {
	for file, want := range tx.digests {
		raw, err := os.ReadFile(filepath.Join(tx.dir, file))
		got := ""
		if err == nil {
			got = digest(raw)
		} else if !os.IsNotExist(err) {
			return err
		}
		if got != want {
			return &ErrConcurrentWrite{File: file}
		}
	}
	for _, file := range tx.order {
		if err := WriteAtomic(filepath.Join(tx.dir, file), tx.staged[file]); err != nil {
			return err
		}
	}
	return nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return string(sum[:])
}
