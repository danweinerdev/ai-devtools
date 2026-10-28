package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type item struct {
	ID    string            `json:"id"`
	Tags  []string          `json:"tags"`
	Note  string            `json:"note,omitempty"`
	Count int               `json:"count,omitempty"`
	Meta  map[string]string `json:"meta,omitempty"`
}

type doc struct {
	Version int       `json:"version"`
	Name    string    `json:"name"`
	Done    bool      `json:"done,omitempty"`
	At      time.Time `json:"at,omitempty"`
	Items   []item    `json:"items"`
}

const validDoc = `{
  "version": 1,
  "name": "work",
  "items": [
    {"id": "a", "tags": ["x"], "meta": {"k": "v"}},
    {"id": "b", "tags": [], "count": 2}
  ]
}`

func problems(t *testing.T, raw string) []string {
	t.Helper()
	var d doc
	err := Decode("tasks.json", []byte(raw), &d, 1)
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("want a DecodeError, got %v", err)
	}
	if de.File != "tasks.json" || !strings.HasPrefix(de.Error(), "tasks.json is invalid:") {
		t.Fatalf("error does not name the file: %v", de)
	}
	return de.Problems
}

func wantProblem(t *testing.T, raw, want string) {
	t.Helper()
	got := problems(t, raw)
	for _, p := range got {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Fatalf("want a problem containing %q, got %q", want, got)
}

func TestDecodeValid(t *testing.T) {
	var d doc
	if err := Decode("tasks.json", []byte(validDoc), &d, 1); err != nil {
		t.Fatal(err)
	}
	if d.Name != "work" || len(d.Items) != 2 || d.Items[0].Meta["k"] != "v" || d.Items[1].Count != 2 {
		t.Fatalf("decoded %+v", d)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"syntax with position", "{\n  \"version\": 1,\n  \"name\": \"w\",,\n}", "line 3, column"},
		{"truncated", `{"version": 1, "name": "w"`, "unexpected end of file"},
		{"trailing data", validDoc + `{}`, "unexpected data after the top-level value"},
		{"not an object", `[1]`, "top level must be a JSON object"},
		{"missing version", `{"name": "w", "items": []}`, "missing key `version`"},
		{"other version", `{"version": 2, "name": "w", "items": []}`, "`version` is 2; this aid reads version 1"},
		{"version as string", `{"version": "1", "name": "w", "items": []}`, "this aid reads version 1"},
		{"unknown key with suggestion", `{"version": 1, "name": "w", "itmes": []}`, "unknown key `itmes` (did you mean `items`?)"},
		{"unknown key without suggestion", `{"version": 1, "name": "w", "items": [], "zzzzzzzz": 1}`, "unknown key `zzzzzzzz`"},
		{"nested unknown key", `{"version": 1, "name": "w", "items": [{"id": "a", "tags": [], "nite": "x"}]}`, "`items[0]`: unknown key `nite` (did you mean `note`?)"},
		{"missing required", `{"version": 1, "items": []}`, "missing required key `name`"},
		{"nested missing required", `{"version": 1, "name": "w", "items": [{"id": "a"}]}`, "`items[0]`: missing required key `tags`"},
		{"null field", `{"version": 1, "name": null, "items": []}`, "`name`: null is not allowed"},
		{"null list", `{"version": 1, "name": "w", "items": null}`, "`items`: null is not allowed"},
		{"null element", `{"version": 1, "name": "w", "items": [{"id": "a", "tags": [null]}]}`, "`items[0].tags[0]`: null is not allowed"},
		{"duplicate key", `{"version": 1, "name": "w", "name": "v", "items": []}`, "duplicate key `name`"},
		{"wrong type string", `{"version": 1, "name": 3, "items": []}`, "`name`: must be a string, got 3"},
		{"wrong type bool", `{"version": 1, "name": "w", "done": "yes", "items": []}`, "`done`: must be true or false"},
		{"wrong type list", `{"version": 1, "name": "w", "items": {}}`, "`items`: must be a list"},
		{"wrong type object", `{"version": 1, "name": "w", "items": ["a"]}`, "`items[0]`: must be an object"},
		{"fractional int", `{"version": 1, "name": "w", "items": [{"id": "a", "tags": [], "count": 1.5}]}`, "`items[0].count`: must be an integer, got 1.5"},
		{"bad timestamp", `{"version": 1, "name": "w", "at": "yesterday", "items": []}`, "parsing time"},
		{"timestamp not a string", `{"version": 1, "name": "w", "at": 5, "items": []}`, "`at`: must be a string"},
		{"map value type", `{"version": 1, "name": "w", "items": [{"id": "a", "tags": [], "meta": {"k": 1}}]}`, "`items[0].meta.k`: must be a string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { wantProblem(t, c.raw, c.want) })
	}
}

func TestDecodeReportsEveryProblem(t *testing.T) {
	got := problems(t, `{"version": 1, "nme": "w", "items": [{"id": 1, "tags": null}], "extra": true}`)
	for _, want := range []string{"unknown key `nme`", "unknown key `extra`", "missing required key `name`",
		"`items[0].id`: must be a string", "`items[0].tags`: null is not allowed"} {
		found := false
		for _, p := range got {
			found = found || strings.Contains(p, want)
		}
		if !found {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

type stamped struct {
	Version int       `json:"version"`
	Since   time.Time `json:"since,omitzero"`
}

func TestDecodeOmitzeroIsOptional(t *testing.T) {
	var s stamped
	if err := Decode("x.json", []byte(`{"version": 1}`), &s, 1); err != nil {
		t.Fatalf("omitzero field treated as required: %v", err)
	}
	out, err := Encode("x.json", s, 1)
	if err != nil || strings.Contains(string(out), "since") {
		t.Fatalf("zero omitzero field encoded: %s %v", out, err)
	}
}

func TestEncodeDeterministicRoundTrip(t *testing.T) {
	d := doc{Version: 1, Name: "a <b> & c", Items: []item{{ID: "x", Tags: []string{"t"}, Meta: map[string]string{"z": "1", "a": "2"}}}}
	first, err := Encode("tasks.json", &d, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := Encode("tasks.json", d, 1)
	if string(first) != string(second) {
		t.Fatal("encoding is not deterministic")
	}
	s := string(first)
	if !strings.HasSuffix(s, "}\n") || !strings.Contains(s, "\n  \"name\": \"a <b> & c\"") {
		t.Fatalf("want two-space indent, no HTML escaping, trailing newline:\n%s", s)
	}
	if strings.Index(s, `"a": "2"`) > strings.Index(s, `"z": "1"`) {
		t.Fatalf("map keys not sorted:\n%s", s)
	}
	var back doc
	if err := Decode("tasks.json", first, &back, 1); err != nil {
		t.Fatal(err)
	}
	if back.Name != d.Name || back.Items[0].Meta["z"] != "1" {
		t.Fatalf("round trip lost data: %+v", back)
	}
}

func TestEncodeRefusesUnreadableOutput(t *testing.T) {
	// A nil required slice encodes as null, which Decode rejects.
	_, err := Encode("tasks.json", doc{Version: 1, Name: "w"}, 1)
	if err == nil || !strings.Contains(err.Error(), "can't read back") || !strings.Contains(err.Error(), "`items`: null") {
		t.Fatalf("want a refusal naming the null, got %v", err)
	}
}

func TestAtomicWriteReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	for _, content := range []string{"first\n", "second\n"} {
		if err := WriteAtomic(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != content {
			t.Fatalf("got %q want %q", got, content)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	assertNoTemps(t, dir)
}

func TestAtomicFailedReplaceLeavesTargetIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := WriteAtomic(path, []byte("original\n")); err != nil {
		t.Fatal(err)
	}
	err := writeAtomicWith(path, []byte("new\n"), func(string, string) error { return errors.New("disk on fire") })
	if err == nil || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("want the replace error, got %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "original\n" {
		t.Fatalf("target changed to %q", got)
	}
	assertNoTemps(t, dir)
}

func TestAtomicSweepRemovesOnlyAbandonedTemps(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		".tasks.json.aid-tmp-123": "partial",
		".state.json.aid-tmp-9":   "partial",
		"tasks.json":              "{}",
		".gitignore":              "*.lock",
		"notes.aid-tmp-1":         "not ours: no leading dot",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	swept, err := SweepTemps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(swept) != 2 {
		t.Fatalf("swept %v", swept)
	}
	for _, keep := range []string{"tasks.json", ".gitignore", "notes.aid-tmp-1"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s was removed", keep)
		}
	}
	assertNoTemps(t, dir)
}

func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && strings.Contains(e.Name(), tempMarker) {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}
