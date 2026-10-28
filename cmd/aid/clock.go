//go:build !aidtest

package main

import "time"

// clock is the real clock. The test build (tag aidtest) swaps in one that
// AID_NOW can pin; the shipped binary never reads AID_NOW.
func clock() func() time.Time { return time.Now }
