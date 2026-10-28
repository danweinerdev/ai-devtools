package graph

import (
	"path"
	"strings"
)

// Covers reports whether a footprint entry covers path. Both are slash paths
// relative to the repository root. An entry covers itself and everything under
// it, comparing whole segments, with or without a trailing slash: `cmd/aid`
// and `cmd/aid/` both cover `cmd/aid/main.go`, but `cmd/ai` doesn't.
func Covers(entry, p string) bool {
	if entry == "" {
		return false // an empty entry covers nothing (path.Clean would make it ".")
	}
	entry, p = path.Clean(entry), path.Clean(p)
	if entry == "." {
		return true
	}
	return p == entry || strings.HasPrefix(p, entry+"/")
}

// Touching returns the tasks that aren't dropped and whose footprint covers at
// least one of paths, in file order.
func (g *Graph) Touching(paths []string) []Task {
	var out []Task
	for _, t := range g.Tasks {
		if t.State.Status == Dropped {
			continue
		}
		if touches(t, paths) {
			out = append(out, t)
		}
	}
	return out
}

func touches(t Task, paths []string) bool {
	for _, entry := range t.Footprint {
		for _, p := range paths {
			if Covers(entry, p) {
				return true
			}
		}
	}
	return false
}
