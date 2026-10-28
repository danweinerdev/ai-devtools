//go:build !windows

package store

import (
	"os"

	"golang.org/x/sys/unix"
)

// flock, not fcntl record locks: record locks are per process and drop when
// any descriptor on the file closes, so an unrelated open elsewhere in the
// process would silently release them. flock is per open file description, so
// each lock lives exactly as long as the fileLock that owns it. LOCK_NB keeps
// the wait loop in Go.

func lockShared(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB)
}

func lockExclusive(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
