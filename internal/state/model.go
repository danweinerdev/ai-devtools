// Package state is the state record held in `.aid/<name>/state.json`: the
// notes, follow-ups, deferred findings and questions that aren't tasks but
// must be followed to the end. The work isn't finished while any is open.
package state

import (
	"fmt"
	"strconv"
)

const (
	// File is the state record's file name inside a work directory.
	File = "state.json"
	// Version is the schema version this aid reads and writes.
	Version = 1
)

// Kinds of item.
const (
	Note     = "note"
	FollowUp = "follow-up"
	Finding  = "finding"
	Question = "question"
)

// Statuses.
const (
	Open     = "open"
	Resolved = "resolved"
)

// Where a resolved item went: its durable home.
const (
	AsDone     = "done"
	AsTask     = "task"
	AsPRD      = "prd"
	AsGuidance = "guidance"
)

var (
	kinds       = []string{Note, FollowUp, Finding, Question}
	statuses    = []string{Open, Resolved}
	resolutions = []string{AsDone, AsTask, AsPRD, AsGuidance}
)

// Kinds, Statuses and Resolutions list the allowed values, for flag help and
// errors.
func Kinds() []string       { return append([]string(nil), kinds...) }
func Statuses() []string    { return append([]string(nil), statuses...) }
func Resolutions() []string { return append([]string(nil), resolutions...) }

// Record is the whole file. Item order is the order items were added.
type Record struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

// Item is one thing noticed that must be followed to the end.
type Item struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Text       string     `json:"text"`
	Resolution Resolution `json:"resolution,omitzero"`
}

// Resolution says where a resolved item went: As is the kind of home, Where
// the specifics (a task ID for `task`, otherwise free text).
type Resolution struct {
	As    string `json:"as"`
	Where string `json:"where"`
}

// IsZero reports an absent resolution; encoding/json's omitzero uses it.
func (r Resolution) IsZero() bool { return r.As == "" && r.Where == "" }

// ByID returns the position of the item with id, or -1.
func (r *Record) ByID(id string) int {
	for i, it := range r.Items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

// NextID is one past the highest ID ever assigned. IDs are never reused:
// resolved items stay in the record, so the highest is always still there.
func (r *Record) NextID() string {
	highest := 0
	for _, it := range r.Items {
		if n, ok := number(it.ID); ok && n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("i%d", highest+1)
}

// number parses `i<n>`.
func number(id string) (int, bool) {
	if len(id) < 2 || id[0] != 'i' {
		return 0, false
	}
	n, err := strconv.Atoi(id[1:])
	return n, err == nil
}
