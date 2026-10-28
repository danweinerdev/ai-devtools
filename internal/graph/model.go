// Package graph is the task graph held in `.aid/<name>/tasks.json`: its
// schema, its validation, and the ready frontier. It never reads the clock;
// callers pass the current time.
package graph

import "time"

const (
	// File is the graph's file name inside a work directory.
	File = "tasks.json"
	// Version is the schema version this aid reads and writes.
	Version = 1
)

// Statuses. Only the tool changes them, through claim, release, complete,
// block and drop.
const (
	Todo       = "todo"
	InProgress = "in-progress"
	Done       = "done"
	Blocked    = "blocked"
	Dropped    = "dropped"
)

var statuses = []string{Todo, InProgress, Done, Blocked, Dropped}

// Graph is the whole file. Task order is the order tasks were added, and is
// preserved.
type Graph struct {
	Version int    `json:"version"`
	Tasks   []Task `json:"tasks"`
}

// Task keeps what the author writes at the top level and what the tool owns
// inside State, so an edit to one can never clobber the other. History is
// tool-owned too, but sits outside State because a status change replaces
// State whole.
type Task struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Needs     []string `json:"needs"`
	Footprint []string `json:"footprint"`
	Done      DoneWhen `json:"done"`
	State     State    `json:"state"`
	History   []Change `json:"history,omitempty"`
}

// Change records one weakening of a task's definition of done: when, by
// whom, why, and the old and new values of whatever it changed.
type Change struct {
	At         time.Time `json:"at"`
	By         string    `json:"by"`
	Reason     string    `json:"reason"`
	ChecksFrom []string  `json:"checks_from,omitempty"`
	ChecksTo   []string  `json:"checks_to,omitempty"`
	ResultFrom string    `json:"result_from,omitempty"`
	ResultTo   string    `json:"result_to,omitempty"`
}

// DoneWhen is the definition of done: the measurable result and the checks
// that prove it.
type DoneWhen struct {
	Result string   `json:"result"`
	Checks []string `json:"checks"`
}

// State is tool-owned.
type State struct {
	Status string `json:"status"`
	Claim  Claim  `json:"claim,omitzero"`
	Reason string `json:"reason,omitempty"`
}

// Claim is a holder's lease on a `in-progress` task.
type Claim struct {
	Holder  string    `json:"holder"`
	Expires time.Time `json:"expires"`
}

// IsZero reports an absent claim; encoding/json's omitzero uses it.
func (c Claim) IsZero() bool {
	return c.Holder == "" && c.Expires.IsZero()
}

// ByID returns the task with id and whether it exists.
func (g *Graph) ByID(id string) (Task, bool) {
	for _, t := range g.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}
