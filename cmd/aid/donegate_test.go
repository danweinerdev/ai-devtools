package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// The definition-of-done gate end to end, through the binary: aid judges an
// update-task call against the real graph, Claude Code gets "ask" for a
// weakening, OpenCode gets an ask verdict naming the task, and file-tool
// edits of the graph or state record are denied.
func TestDefinitionOfDoneGate(t *testing.T) {
	r := newRepo(t)
	r.ok("init", "Feature")
	r.ok("add-task", "--id", "a", "--title", "t", "--footprint", "a.go", "--result", "a works",
		"--check", "go test ./a")
	sub := filepath.Join(r.dir, "pkg")
	writeFile(t, filepath.Join(sub, "x.go"), "package x\n")

	bash := func(command string) string {
		b, err := json.Marshal(map[string]any{"tool_name": "Bash", "cwd": sub,
			"tool_input": map[string]any{"command": command}})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	asks := func(command string) bool {
		t.Helper()
		out, errOut, code := guardRun(t, sub, "definition-of-done", bash(command), "AID_GUARDS=")
		if code != 0 {
			t.Fatalf("%s: exit %d %s", command, code, errOut)
		}
		return strings.Contains(out, `"permissionDecision":"ask"`)
	}

	for _, c := range []struct {
		command string
		ask     bool
	}{
		{`aid update-task a --check true`, true},
		{`aid update-task a --check 'go test ./a -count=1'`, true},
		{`aid update-task a --result "a compiles"`, true},
		{`AID_HOLDER=aid-mechanic /usr/local/bin/aid --work Feature update-task a --check=true --reason x`, true},
		{`aid update-task a --check 'go test ./a' --check 'go vet ./a'`, false}, // only adds proof
		{`aid update-task a --result 'a works'`, false},                         // unchanged
		{`aid update-task a --title renamed`, false},
		{`aid update-task nope --check true`, false}, // aid refuses it itself
		{`aid show a`, false},
		{`grep -n update-task notes.md --check`, false}, // not aid
		{`cd .. && aid update-task a --check true`, true},
		{`aid update-task a --check "$WEAK"`, true},
		{`aid update-task a --title "$(date)"`, false}, // can't parse, but touches neither flag
	} {
		if got := asks(c.command); got != c.ask {
			t.Errorf("%s: ask = %v, want %v", c.command, got, c.ask)
		}
	}
	out, _, _ := guardRun(t, sub, "definition-of-done", bash(`aid update-task a --check true`), "AID_GUARDS=")
	if !strings.Contains(out, "removes or changes the check(s) `go test ./a` of task a") {
		t.Errorf("the question doesn't say what's weakened: %s", out)
	}

	edit := func(tool, path string) int {
		b, _ := json.Marshal(map[string]any{"tool_name": tool, "cwd": sub,
			"tool_input": map[string]any{"file_path": path, "content": "{}"}})
		_, _, code := guardRun(t, sub, "definition-of-done", string(b), "AID_GUARDS=")
		return code
	}
	for _, c := range []struct {
		tool, path string
		code       int
	}{
		{"Write", "../.aid/Feature/tasks.json", 2},
		{"Edit", filepath.Join(r.dir, ".aid", "Feature", "state.json"), 2},
		{"MultiEdit", "../.aid/Feature/tasks.json", 2},
		{"Write", "../.aid/Feature/breakdown.md", 0},
		{"Write", "x.go", 0},
		{"Read", "../.aid/Feature/tasks.json", 0},
	} {
		if got := edit(c.tool, c.path); got != c.code {
			t.Errorf("%s %s: exit %d, want %d", c.tool, c.path, got, c.code)
		}
	}
	if _, _, code := guardRun(t, sub, "definition-of-done", bash(`aid update-task a --check true`), "AID_GUARDS=off"); code != 0 {
		t.Errorf("AID_GUARDS=off still gated")
	}

	// OpenCode: the same judgement as an ask verdict naming the task, and its
	// file tools (write, edit, patch) denied on the records.
	verdict := func(tool string, args map[string]any) map[string]string {
		b, _ := json.Marshal(map[string]any{"tool": tool, "args": args, "sessionID": "s", "agent": "aid-mechanic", "cwd": sub})
		out, errOut, code := aidRun(t, sub, string(b), nil, "guard", "opencode-tool")
		if code != 0 {
			t.Fatalf("opencode-tool: exit %d %s", code, errOut)
		}
		var v map[string]string
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := verdict("shell", map[string]any{"command": "aid update-task a --check true"}); v["decision"] != "ask" ||
		v["task"] != "a" || !strings.Contains(v["reason"], "weakens its definition of done") {
		t.Errorf("shell weakening: %v", v)
	}
	if v := verdict("shell", map[string]any{"command": "aid update-task a --check 'go test ./a' --check x"}); v["decision"] != "allow" {
		t.Errorf("shell strengthening: %v", v)
	}
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"write", map[string]any{"path": "../.aid/Feature/tasks.json", "content": "{}"}},
		{"edit", map[string]any{"filePath": "../.aid/Feature/state.json"}},
		{"patch", map[string]any{"patchText": "*** Begin Patch\n*** Update File: ../.aid/Feature/tasks.json\n@@\n-a\n+b\n*** End Patch"}},
	} {
		if v := verdict(c.tool, c.args); v["decision"] != "deny" {
			t.Errorf("%s on a record: %v", c.tool, v)
		}
	}
	if v := verdict("patch", map[string]any{"patchText": "*** Begin Patch\n*** Add File: pkg/y.go\n+package x\n*** End Patch"}); v["decision"] != "allow" {
		t.Errorf("patch elsewhere: %v", v)
	}
}
