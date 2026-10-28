//go:build !windows && !linux

package checks

import "syscall"

// setDeathSignal is a no-op: only Linux has a parent-death signal.
func setDeathSignal(*syscall.SysProcAttr) {}
