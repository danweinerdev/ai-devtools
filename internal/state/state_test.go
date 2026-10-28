package state

import (
	"strings"
	"testing"

	"github.com/danweinerdev/ai-devtools/internal/store"
)

func open(id, kind, text string) Item {
	return Item{ID: id, Kind: kind, Status: Open, Text: text}
}

func resolved(id, as, where string) Item {
	return Item{ID: id, Kind: FollowUp, Status: Resolved, Text: "t", Resolution: Resolution{As: as, Where: where}}
}

func TestValidateAcceptsAValidRecord(t *testing.T) {
	r := &Record{Version: Version, Items: []Item{open("i1", Note, "n"), resolved("i2", AsTask, "store-lock")}}
	if p := r.Validate(); len(p) > 0 {
		t.Fatal(p)
	}
	if p := (&Record{Version: Version, Items: []Item{}}).Validate(); len(p) > 0 {
		t.Fatal(p)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		item Item
		want string
	}{
		{"id pattern", open("x1", Note, "t"), `id "x1" must match`},
		{"id zero", open("i0", Note, "t"), `id "i0" must match`},
		{"id leading zero", open("i01", Note, "t"), `id "i01" must match`},
		{"kind", open("i1", "todo", "t"), `kind "todo" must be one of`},
		{"empty text", open("i1", Note, "  "), "text is empty"},
		{"status", Item{ID: "i1", Kind: Note, Status: "closed", Text: "t"}, `status "closed" must be one of`},
		{"open with resolution", Item{ID: "i1", Kind: Note, Status: Open, Text: "t", Resolution: Resolution{As: AsDone, Where: "x"}}, "an open item has no resolution"},
		{"resolved without resolution", Item{ID: "i1", Kind: Note, Status: Resolved, Text: "t"}, "needs a resolution"},
		{"resolution as", resolved("i1", "elsewhere", "x"), `resolution.as "elsewhere" must be one of`},
		{"resolution where", resolved("i1", AsPRD, ""), "resolution.where is empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := (&Record{Version: Version, Items: []Item{c.item}}).Validate()
			if !strings.Contains(strings.Join(p, "\n"), c.want) {
				t.Fatalf("want %q, got %q", c.want, p)
			}
		})
	}
	p := (&Record{Version: Version, Items: []Item{open("i1", Note, "a"), open("i1", Note, "b")}}).Validate()
	if !strings.Contains(strings.Join(p, "\n"), "item `i1`: duplicate id") {
		t.Fatalf("duplicate: %q", p)
	}
}

func TestValidateNextIDNeverReuses(t *testing.T) {
	r := &Record{Version: Version, Items: []Item{}}
	if got := r.NextID(); got != "i1" {
		t.Fatalf("first id %s", got)
	}
	r.Items = []Item{resolved("i1", AsDone, "x"), open("i7", Note, "t"), resolved("i3", AsDone, "y")}
	if got := r.NextID(); got != "i8" {
		t.Fatalf("want i8, got %s", got)
	}
	if r.ByID("i3") != 2 || r.ByID("i9") != -1 {
		t.Fatal("ByID")
	}
}

func TestValidateStoreRoundTrip(t *testing.T) {
	r := &Record{Version: Version, Items: []Item{open("i1", Question, "why?"), resolved("i2", AsGuidance, "AGENTS.md")}}
	raw, err := store.Encode(File, r, Version)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"resolution"`) != 1 {
		t.Fatalf("an open item must carry no resolution key:\n%s", raw)
	}
	var back Record
	if err := store.Decode(File, raw, &back, Version); err != nil {
		t.Fatal(err)
	}
	if back.Items[1].Resolution.Where != "AGENTS.md" || back.Items[0].Status != Open {
		t.Fatalf("round trip: %+v", back)
	}
}
