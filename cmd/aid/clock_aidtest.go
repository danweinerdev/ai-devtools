//go:build aidtest

package main

import (
	"os"
	"time"
)

// clock is time.Now, unless AID_NOW holds an RFC 3339 time. Only binaries
// built with the aidtest tag (the end-to-end and golden tests) have this, so
// golden output is reproducible without a stray AID_NOW ever reaching users.
func clock() func() time.Time {
	if t, err := time.Parse(time.RFC3339, os.Getenv("AID_NOW")); err == nil {
		return func() time.Time { return t }
	}
	return time.Now
}
