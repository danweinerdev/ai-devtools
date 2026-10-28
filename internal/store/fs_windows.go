//go:build windows

package store

import (
	"errors"
	"os"
	"syscall"
	"time"
)

const (
	replaceRetryWindow   = 2 * time.Second
	replaceRetryInterval = 10 * time.Millisecond
	errSharingViolation  = syscall.Errno(32)
)

// replaceFile retries a rename that Windows refuses because another process
// (a reader, an indexer, antivirus) briefly holds the target open. The caller
// holds aid's lock, so no other aid writer can change the target meanwhile.
func replaceFile(oldpath, newpath string) error {
	deadline := time.Now().Add(replaceRetryWindow)
	for {
		err := os.Rename(oldpath, newpath)
		if err == nil || !(errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errSharingViolation)) {
			return err
		}
		if !time.Now().Add(replaceRetryInterval).Before(deadline) {
			return err
		}
		time.Sleep(replaceRetryInterval)
	}
}

// syncDir is a no-op: Windows can't open a directory for flushing, and NTFS
// journals the rename's metadata.
func syncDir(string) error { return nil }
