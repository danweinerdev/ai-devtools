package state

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var idPattern = regexp.MustCompile(`^i[1-9][0-9]*$`)

// Validate reports every rule the record breaks, each located by item. It
// implements store.Validator, so it runs on every load and before every write.
func (r *Record) Validate() []string {
	var problems []string
	add := func(at, format string, args ...any) {
		problems = append(problems, at+": "+fmt.Sprintf(format, args...))
	}
	seen := map[string]bool{}
	for i, it := range r.Items {
		at := fmt.Sprintf("item `%s`", it.ID)
		switch {
		case !idPattern.MatchString(it.ID):
			add(fmt.Sprintf("items[%d]", i), "id %q must match %s", it.ID, idPattern)
		case seen[it.ID]:
			add(at, "duplicate id")
		default:
			seen[it.ID] = true
		}
		if !slices.Contains(kinds, it.Kind) {
			add(at, "kind %q must be one of %s", it.Kind, strings.Join(kinds, ", "))
		}
		if strings.TrimSpace(it.Text) == "" {
			add(at, "text is empty")
		}
		switch it.Status {
		case Open:
			if !it.Resolution.IsZero() {
				add(at, "an open item has no resolution")
			}
		case Resolved:
			if it.Resolution.IsZero() {
				add(at, "a resolved item needs a resolution saying where it went")
				break
			}
			if !slices.Contains(resolutions, it.Resolution.As) {
				add(at, "resolution.as %q must be one of %s", it.Resolution.As, strings.Join(resolutions, ", "))
			}
			if strings.TrimSpace(it.Resolution.Where) == "" {
				add(at, "resolution.where is empty")
			}
		default:
			add(at, "status %q must be one of %s", it.Status, strings.Join(statuses, ", "))
		}
	}
	return problems
}
