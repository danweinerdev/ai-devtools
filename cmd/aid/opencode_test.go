package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// aidRun runs the binary in dir with stdin and extra env, isolating the
// global OpenCode config: XDG_CONFIG_HOME points at an empty temp dir unless
// env sets it.
func aidRun(t *testing.T, dir, stdin string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(aidBin, args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir(), "AID_GUARDS=")
	cmd.Env = append(cmd.Env, env...)
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeConfigThroughTheBinary(t *testing.T) {
	dir := t.TempDir()

	// No file: source null, empty maps, exit 0.
	out, errOut, code := aidRun(t, dir, "", nil, "config", "--harness", "opencode", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var empty map[string]any
	if err := json.Unmarshal([]byte(out), &empty); err != nil {
		t.Fatal(err)
	}
	if empty["source"] != nil || len(empty["profiles"].(map[string]any)) != 0 || len(empty["agents"].(map[string]any)) != 0 ||
		empty["hide_free_models"] != false {
		t.Fatalf("no file: %s", out)
	}

	// A global file, found through XDG_CONFIG_HOME.
	xdg := t.TempDir()
	global := filepath.Join(xdg, "opencode", "aid.json")
	writeFile(t, global, `{"profiles": {"fast": {"model": "anthropic/claude-haiku-5-5#low"}}, "agents": {"aid-explorer": "fast"}, "hide_free_models": true}`)
	out, errOut, code = aidRun(t, dir, "", []string{"XDG_CONFIG_HOME=" + xdg}, "config", "--harness", "opencode", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got struct {
		Source   string `json:"source"`
		Profiles map[string]struct {
			Model, Provider, ID, Variant string
		} `json:"profiles"`
		Agents         map[string]string `json:"agents"`
		HideFreeModels bool              `json:"hide_free_models"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	p := got.Profiles["fast"]
	if got.Source != global || got.Agents["aid-explorer"] != "fast" || !got.HideFreeModels ||
		p.Model != "anthropic/claude-haiku-5-5#low" || p.Provider != "anthropic" || p.ID != "claude-haiku-5-5" || p.Variant != "low" {
		t.Fatalf("global: %s", out)
	}

	// An invalid project file: exit 1, every problem on stderr, even with a valid global.
	writeFile(t, filepath.Join(dir, ".opencode", "aid.json"), `{"profiles": {"fast": {"model": "x"}}, "agents": {"aid-oracle": "deep"}}`)
	_, errOut, code = aidRun(t, dir, "", []string{"XDG_CONFIG_HOME=" + xdg}, "config", "--harness", "opencode", "--json")
	if code != 1 || !strings.Contains(errOut, "profiles.fast.model") || !strings.Contains(errOut, "profile `deep` doesn't exist") {
		t.Fatalf("invalid: exit %d, stderr %q", code, errOut)
	}

	// Claude's side, for symmetry.
	writeFile(t, filepath.Join(dir, ".claude", "aid.json"), `{"models": {"aid-explorer": "sonnet"}}`)
	out, _, code = aidRun(t, dir, "", nil, "config", "--harness", "claude", "--json")
	if code != 0 || !strings.Contains(out, `"aid-explorer": "sonnet"`) || !strings.Contains(out, `"source": "`) {
		t.Fatalf("claude: exit %d, %s", code, out)
	}
}

func TestOpenCodeToolThroughTheBinary(t *testing.T) {
	dir := t.TempDir()
	call := func(env []string, tool string, args map[string]any) (map[string]string, string, int) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"tool": tool, "args": args, "sessionID": "s-" + filepath.Base(dir),
			"agent": "aid-orchestrator", "cwd": dir})
		out, errOut, code := aidRun(t, dir, string(b), env, "guard", "opencode-tool")
		var v map[string]string
		if code == 0 {
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatalf("stdout isn't JSON: %q", out)
			}
		}
		return v, errOut, code
	}

	v, _, code := call(nil, "subagent", map[string]any{"agent": "general", "prompt": "p"})
	if code != 0 || v["decision"] != "deny" || !strings.Contains(v["reason"], "general is blocked") {
		t.Fatalf("general: exit %d %v", code, v)
	}
	v, _, code = call(nil, "subagent", map[string]any{"agent": "aid-explorer", "prompt": "p"})
	if code != 0 || v["decision"] != "allow" {
		t.Fatalf("aid-explorer: exit %d %v", code, v)
	}

	// An invalid .opencode/aid.json denies with what to fix.
	writeFile(t, filepath.Join(dir, ".opencode", "aid.json"), `{"profiles": 3}`)
	v, _, code = call(nil, "subagent", map[string]any{"agent": "aid-explorer", "prompt": "p"})
	if code != 0 || v["decision"] != "deny" || !strings.Contains(v["reason"], ".opencode/aid.json is invalid") {
		t.Fatalf("invalid config: exit %d %v", code, v)
	}

	// AID_GUARDS=off allows; unusable stdin is aid's own failure: non-zero, reason on stderr.
	v, _, code = call([]string{"AID_GUARDS=off"}, "subagent", map[string]any{"agent": "general"})
	if code != 0 || v["decision"] != "allow" {
		t.Fatalf("off: exit %d %v", code, v)
	}
	_, errOut, code := aidRun(t, dir, "not json", nil, "guard", "opencode-tool")
	if code == 0 || !strings.Contains(errOut, "opencode-tool") {
		t.Fatalf("unusable stdin: exit %d %q", code, errOut)
	}
}

func TestPolicyThroughTheBinary(t *testing.T) {
	dir := t.TempDir()
	out, _, code := aidRun(t, dir, "", nil, "guard", "policy", "--harness", "opencode")
	if code != 0 || !strings.Contains(out, "explore -> aid-explorer") || strings.Contains(out, "general-purpose") {
		t.Fatalf("opencode policy: exit %d\n%s", code, out)
	}
	out, _, code = aidRun(t, dir, "", nil, "guard", "policy")
	if code != 0 || !strings.Contains(out, "general-purpose") {
		t.Fatalf("claude policy: exit %d\n%s", code, out)
	}
	if _, errOut, code := aidRun(t, dir, "", nil, "guard", "policy", "--harness", "codex"); code == 0 || !strings.Contains(errOut, "use claude or opencode") {
		t.Fatalf("unknown harness: exit %d %q", code, errOut)
	}
}
