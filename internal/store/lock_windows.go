//go:build windows

package store

import (
	"os"

	"golang.org/x/sys/windows"
)

// LockFileEx over the whole 64-bit range matches flock's whole-file locking.
// LOCKFILE_FAIL_IMMEDIATELY mirrors LOCK_NB, keeping the wait loop in Go.

const lockRange = ^uint32(0)

func lockRangeWith(f *os.File, flags uint32) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, lockRange, lockRange, new(windows.Overlapped))
}

func lockShared(f *os.File) error {
	return lockRangeWith(f, windows.LOCKFILE_FAIL_IMMEDIATELY)
}

func lockExclusive(f *os.File) error {
	return lockRangeWith(f, windows.LOCKFILE_FAIL_IMMEDIATELY|windows.LOCKFILE_EXCLUSIVE_LOCK)
}

func unlockFile(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, lockRange, lockRange, new(windows.Overlapped))
}
