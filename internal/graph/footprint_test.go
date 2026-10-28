package graph

import "testing"

func TestFootprintCovers(t *testing.T) {
	cases := []struct {
		entry, path string
		want        bool
	}{
		{"cmd/aid/main.go", "cmd/aid/main.go", true},
		{"cmd/aid", "cmd/aid/main.go", true},
		{"cmd/aid/", "cmd/aid/main.go", true},
		{"cmd/aid", "cmd/aid", true},
		{"cmd/aid/", "cmd/aid", true},
		{"cmd", "cmd/aid/deep/x.go", true},
		{"cmd/ai", "cmd/aid/main.go", false},
		{"cmd/aid/main.go", "cmd/aid", false},
		{"internal/store", "internal/graph/model.go", false},
		{"a.go", "b.go", false},
		{"./cmd/aid", "cmd/aid/x.go", true},
	}
	for _, c := range cases {
		if got := Covers(c.entry, c.path); got != c.want {
			t.Errorf("Covers(%q, %q) = %v, want %v", c.entry, c.path, got, c.want)
		}
	}
}

func TestFootprintTouching(t *testing.T) {
	a := task("a", Done)
	a.Footprint = []string{"internal/store/"}
	b := task("b", Todo)
	b.Footprint = []string{"cmd/aid", "README.md"}
	c := task("c", Dropped)
	c.State.Reason = "gone"
	c.Footprint = []string{"cmd/aid"}
	g := graphOf(a, b, c)

	for _, tc := range []struct {
		paths []string
		want  string
	}{
		{[]string{"cmd/aid/check.go"}, "b"},
		{[]string{"internal/store/lock.go", "README.md"}, "a,b"},
		{[]string{"cmd/ai"}, ""},
		{[]string{"docs/x.md"}, ""},
		{nil, ""},
	} {
		if got := ids(g.Touching(tc.paths)); got != tc.want {
			t.Errorf("Touching(%v) = %q, want %q", tc.paths, got, tc.want)
		}
	}
}
