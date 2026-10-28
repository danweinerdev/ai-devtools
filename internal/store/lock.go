package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Advisory locking, so several sessions and subagents can drive one piece of
// work without corrupting each other's writes.
//
// There is one lock per work directory (`.aid/<name>/.lock`), not per file:
// `finish` must see the graph and the state together, and a command that
// touches both must not interleave with another.
//
// The lock is a sidecar because WriteAtomic renames over its target, which
// replaces the inode; a lock on the data file itself would protect the file
// that just got replaced. The sidecar's inode is stable and it never holds
// data. The kernel releases the lock when its holder dies, so a crash never
// wedges the work.
//
// Writers take it exclusively for the whole load → validate → mutate →
// validate → write cycle, and time out with a clear error rather than hang.
// Readers take it shared, retry briefly, then read anyway: atomic writes
// leave only complete versions on disk, and a stuck writer shouldn't make the
// work uninspectable.

const (
	// LockFile is the sidecar in each work directory. `.aid/.gitignore`
	// excludes it.
	LockFile = ".lock"

	lockRetryInterval = 10 * time.Millisecond
	readLockWindow    = 2 * time.Second
	writeLockTimeout  = 10 * time.Second
)

// ErrLockTimeout reports that the write lock couldn't be taken in time.
var ErrLockTimeout = errors.New("timed out waiting for the work lock")

type fileLock struct{ f *os.File }

func (l *fileLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = unlockFile(l.f)
	_ = l.f.Close()
	l.f = nil
}

func openLock(dir string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, LockFile), os.O_CREATE|os.O_RDWR, 0o644)
}

// acquire polls the non-blocking lock call until it succeeds or the deadline
// passes; keeping the wait in Go makes the deadline bounded and testable.
func acquire(dir string, exclusive bool, timeout time.Duration) (*fileLock, error) {
	f, err := openLock(dir)
	if err != nil {
		return nil, fmt.Errorf("opening the lock in %s: %w", dir, err)
	}
	try := lockShared
	if exclusive {
		try = lockExclusive
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := try(f); err == nil {
			return &fileLock{f: f}, nil
		}
		if !time.Now().Before(deadline) {
			f.Close()
			return nil, ErrLockTimeout
		}
		time.Sleep(lockRetryInterval)
	}
}
