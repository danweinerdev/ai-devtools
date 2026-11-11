package main

import "testing"

// TestClaimSessionRecordedAndShown covers the CLI side of the session: a claim
// with --session records it, a later claim without one leaves it, every status
// change keeps it, and show prints it.
func TestClaimSessionRecordedAndShown(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	r.step("add-task", "--id", "a", "--title", "t", "--footprint", "x", "--result", "r", "--check", "true")

	out := r.ok("claim-task", "a", "--holder", "h", "--session", "ses_build", "--json")
	contains(t, out, `"session": "ses_build"`)
	contains(t, r.ok("show", "a"), "session:    ses_build")

	// Claiming again without --session renews the lease and keeps the session.
	r.ok("claim-task", "a", "--holder", "h")
	contains(t, r.ok("show", "a"), "session:    ses_build")

	// A later --session replaces it.
	r.ok("claim-task", "a", "--holder", "h", "--session", "ses_follow_up")
	contains(t, r.ok("show", "a"), "session:    ses_follow_up")

	// Completing keeps it.
	r.ok("complete-task", "a", "--holder", "h")
	out = r.ok("show", "a", "--json")
	contains(t, out, `"session": "ses_follow_up"`)
}

// TestClaimSessionEmptyTreatedAsNotGiven checks that an empty --session
// records nothing and never clears a session already there.
func TestClaimSessionEmptyTreatedAsNotGiven(t *testing.T) {
	r := newRepo(t)
	r.step("init", "Feature")
	r.step("add-task", "--id", "a", "--title", "t", "--footprint", "x", "--result", "r", "--check", "true")
	r.ok("claim-task", "a", "--holder", "h", "--session", "ses_build")
	r.ok("claim-task", "a", "--holder", "h", "--session=")
	contains(t, r.ok("show", "a"), "session:    ses_build")
}
