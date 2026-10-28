package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// guardRun pipes stdin through `aid guard <name>` in dir with extra env.
func guardRun(t *testing.T, dir, name, stdin string, env ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(aidBin, "guard", name)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), env...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

func spawnJSON(t *testing.T, cwd, subagentType string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"tool_name": "Agent", "cwd": cwd,
		"tool_input": map[string]any{"subagent_type": subagentType, "prompt": "p"}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGuardThroughTheBinary(t *testing.T) {
	dir := t.TempDir() // no .aid/: guards never need a work
	out, errOut, code := guardRun(t, dir, "generic", spawnJSON(t, dir, "general-purpose"))
	if code != 2 || out != "" || !strings.Contains(errOut, "aid-guards: general-purpose is blocked") {
		t.Fatalf("generic: exit %d out %q err %q", code, out, errOut)
	}
	if strings.Contains(errOut, "aid: ") {
		t.Fatalf("the deny is the guard's own message, not an aid error: %q", errOut)
	}
	if _, _, code := guardRun(t, dir, "generic", spawnJSON(t, dir, "general-purpose"), "AID_GUARDS=off"); code != 0 {
		t.Fatalf("AID_GUARDS=off: exit %d", code)
	}

	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "aid.json"), []byte(`{"models": {"aid-explorer": "sonnet"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code = guardRun(t, dir, "models", spawnJSON(t, dir, "aid-explorer"))
	var res struct {
		H struct {
			PermissionDecision string         `json:"permissionDecision"`
			UpdatedInput       map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || code != 0 ||
		res.H.PermissionDecision != "allow" || res.H.UpdatedInput["model"] != "sonnet" || res.H.UpdatedInput["prompt"] != "p" {
		t.Fatalf("models: exit %d %v %q", code, err, out)
	}

	if err := os.WriteFile(filepath.Join(dir, ".claude", "aid.json"), []byte(`{"modles": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut, code = guardRun(t, dir, "models", spawnJSON(t, dir, "aid-explorer"))
	if code != 2 || !strings.Contains(errOut, "did you mean `models`") {
		t.Fatalf("bad config: exit %d %q", code, errOut)
	}
	out, _, code = guardRun(t, dir, "session-start", "{}")
	if code != 0 || !strings.Contains(out, "Configuration error") {
		t.Fatalf("session-start: exit %d %q", code, out)
	}

	if help, _, _ := guardRun(t, dir, "--help", ""); !strings.Contains(help, "inline") {
		t.Fatalf("guard --help: %q", help)
	}
	r := newRepo(t)
	if main := r.ok("--help"); strings.Contains(main, "guard") {
		t.Fatalf("guard should be hidden from the main help:\n%s", main)
	}
}

// --- the smoke run: the wired hooks, exactly as Claude Code runs them ----------

// findBash is the bash Claude Code runs hooks with: Git Bash on Windows, never
// System32's WSL launcher, which a bare `bash` can resolve to there.
func findBash() string {
	if b := os.Getenv("AID_SMOKE_BASH"); b != "" {
		return b
	}
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMW6432")} {
			if base == "" {
				continue
			}
			if p := filepath.Join(base, "Git", "bin", "bash.exe"); fileExists(p) {
				return p
			}
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

type wiredHook struct {
	event, matcher, command string
}

func wiredHooks(t *testing.T, root string) []wiredHook {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "harness", "claude", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	var hooks []wiredHook
	for _, event := range []string{"SessionStart", "PreToolUse"} {
		for _, block := range manifest.Hooks[event] {
			for _, h := range block.Hooks {
				if h.Type == "command" {
					hooks = append(hooks, wiredHook{event, block.Matcher, h.Command})
				}
			}
		}
	}
	return hooks
}

// aidCommand matches the `aid` at the start of a wired command.
var aidCommand = regexp.MustCompile(`^aid(\s)`)

func TestGuardSmokeWiredHooks(t *testing.T) {
	bash := findBash()
	if bash == "" {
		t.Skip("no bash to run hooks with")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	hooks := wiredHooks(t, root)
	if len(hooks) != 6 {
		t.Fatalf("want 6 wired hooks, got %d", len(hooks))
	}
	bin := `"` + filepath.ToSlash(aidBin) + `"`
	cwd := t.TempDir()
	run := func(command, stdin string, env ...string) (string, string, int) {
		t.Helper()
		if !aidCommand.MatchString(command) {
			t.Fatalf("hook doesn't run aid: %q", command)
		}
		cmd := exec.Command(bash, "-c", aidCommand.ReplaceAllString(command, bin+"$1"))
		cmd.Dir = cwd
		cmd.Stdin = strings.NewReader(stdin)
		cmd.Env = append(os.Environ(), append([]string{"CLAUDE_PLUGIN_ROOT=" + root}, env...)...)
		var out, errOut strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errOut
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), errOut.String(), code
	}
	crashed := func(stderr string) bool {
		return strings.Contains(stderr, "goroutine ") || strings.Contains(stderr, "panic:")
	}

	unusable := []string{"not json {{{", "", `{"tool_name": "Agent"`, "null", "[1, 2]", `"s"`}
	odd := []string{`{}`, `{"tool_name": 42, "tool_input": "nope", "cwd": null}`,
		`{"tool_name": "Agent", "tool_input": null}`,
		`{"tool_name": "Agent", "tool_input": {"subagent_type": ["x"], "prompt": {"a": [1]}}}`}
	for _, h := range hooks {
		label := h.event + "[" + h.matcher + "]"
		for _, raw := range unusable {
			if _, errOut, code := run(h.command, raw); code != 0 || crashed(errOut) {
				t.Errorf("%s didn't fail open on %q: exit %d %q", label, raw, code, errOut)
			}
		}
		for _, raw := range odd {
			if _, errOut, code := run(h.command, raw); (code != 0 && code != 2) || crashed(errOut) {
				t.Errorf("%s exit %d on %s: %q", label, code, raw, errOut)
			}
		}
	}

	var agent []string
	for _, h := range hooks {
		if h.matcher == "Agent|Task" {
			agent = append(agent, h.command)
		}
	}
	codes := func(subagentType string, env ...string) []int {
		var out []int
		for _, c := range agent {
			_, _, code := run(c, spawnJSON(t, cwd, subagentType), env...)
			out = append(out, code)
		}
		return out
	}
	if !slices.Contains(codes("general-purpose"), 2) {
		t.Error("general-purpose spawn was not denied")
	}
	if slices.ContainsFunc(codes("ai-development:aid-oracle"), func(c int) bool { return c != 0 }) {
		t.Error("namespaced aid-oracle spawn was denied")
	}
	if slices.ContainsFunc(codes("general-purpose", "AID_GUARDS=off"), func(c int) bool { return c != 0 }) {
		t.Error("AID_GUARDS=off did not allow")
	}

	if err := os.MkdirAll(filepath.Join(cwd, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".claude", "aid.json"), []byte(`{"models": {"aid-explorer": "sonnet"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var models []string
	for _, c := range agent {
		out, _, _ := run(c, spawnJSON(t, cwd, "aid-explorer"))
		if strings.TrimSpace(out) == "" {
			continue
		}
		var res struct {
			H struct {
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("hook stdout isn't JSON: %q", out)
		}
		models = append(models, res.H.UpdatedInput["model"].(string))
	}
	if !slices.Equal(models, []string{"sonnet"}) {
		t.Errorf("model override not applied exactly once: %v", models)
	}

	for _, h := range hooks {
		if h.event != "SessionStart" {
			continue
		}
		out, _, _ := run(h.command, "{}")
		if !strings.Contains(out, "Routing policy") || !strings.Contains(out, "aid-explorer") ||
			!strings.Contains(out, "You are the orchestrator") {
			t.Errorf("SessionStart didn't print the orchestrate skill and the routing policy:\n%s", out)
		}
	}
}
