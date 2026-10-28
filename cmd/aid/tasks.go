package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/danweinerdev/ai-devtools/internal/graph"
	"github.com/danweinerdev/ai-devtools/internal/store"
)

// loadGraph is the one way commands read the graph: strict decode plus full
// validation, every time.
func loadGraph(tx *store.Tx) (*graph.Graph, error) {
	var g graph.Graph
	if err := tx.Load(graph.File, &g, graph.Version); err != nil {
		return nil, err
	}
	return &g, nil
}

func saveGraph(tx *store.Tx, g *graph.Graph) error {
	return tx.Save(graph.File, g, graph.Version)
}

// index returns the position of id in g, or an error naming the known IDs'
// count so a typo is obvious.
func index(g *graph.Graph, id string) (int, error) {
	for i, t := range g.Tasks {
		if t.ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("no task %q (%d tasks; `aid show` lists them)", id, len(g.Tasks))
}

// taskView is a task as commands print it with --json: the stored task plus
// its effective status at the time of the command.
type taskView struct {
	graph.Task
	Effective string `json:"effective"`
}

func view(t graph.Task, now time.Time) taskView {
	return taskView{Task: t, Effective: t.Effective(now)}
}

// newID is a lowercase UUIDv7: time-ordered, so generated IDs sort by
// creation, and matching the graph's ID pattern. It fails rather than return
// a zeroed ID if the random source is unavailable.
func newID(now time.Time) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("can't generate an ID: %w", err)
	}
	ms := uint64(now.UnixMilli())
	for i := 0; i < 6; i++ {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = b[6]&0x0f | 0x70 // version 7
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b[:])
	return strings.Join([]string{h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]}, "-"), nil
}
