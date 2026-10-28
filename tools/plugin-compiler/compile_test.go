package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const orchestrateSkill = `---
name: aid-orchestrate
description: Orchestration rules, with a colon: and <html> & quotes "x".
user-invocable: false
---

# Orchestrate

**Loop:** all

Delegate to aid-explorer.
`

const thinkSkill = `---
name: aid-think-explore
description: Explore stage of the think loop.
user-invocable: false
---

**Loop:** think
`

const explorerAgent = `---
name: aid-explorer
description: Read-only research.
loop: think
model: haiku
readOnly: true
disallowedTools: Write, Edit, NotebookEdit
capabilities: [research, locate]
---

You are the explorer. See aid-orchestrate.
`

const mechanicAgent = `---
name: aid-mechanic
description: 'Builds what''s specified.'
loop: make
model: sonnet
readOnly: false
capabilities:
  - implement
  - test
---

You are the mechanic.
`

// fixture is a minimal valid repository: the real harness config, two skills,
// and a read-only and a writing agent.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg, err := os.ReadFile(filepath.Join("..", "..", "config", "harness.json"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "config/harness.json", string(cfg))
	write(t, root, "skills/aid-orchestrate/SKILL.md", orchestrateSkill)
	write(t, root, "skills/aid-think-explore/SKILL.md", thinkSkill)
	write(t, root, "agents/aid-explorer.md", explorerAgent)
	write(t, root, "agents/aid-mechanic.md", mechanicAgent)
	return root
}

func write(t *testing.T, root, path, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func compile(root string, check bool) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(root, check, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRepositoryIsInSync(t *testing.T) {
	code, out, errOut := compile(filepath.Join("..", ".."), true)
	if code != 0 {
		t.Fatalf("repo out of sync or invalid (run `make build`):\n%s", errOut)
	}
	if !strings.HasPrefix(out, "checked: ") {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestBuildThenCheck(t *testing.T) {
	root := fixture(t)
	if code, _, errOut := compile(root, true); code != 1 || !strings.Contains(errOut, "skills-manifest.json") {
		t.Fatalf("check before build: want stale manifest, got %d %q", code, errOut)
	}
	if code, out, errOut := compile(root, false); code != 0 || out != "built: 2 skills, 2 agents ("+root+")\n" {
		t.Fatalf("build: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := compile(root, true); code != 0 {
		t.Fatalf("check after build: %s", errOut)
	}
}

func TestRender(t *testing.T) {
	root := fixture(t)
	if code, _, errOut := compile(root, false); code != 0 {
		t.Fatal(errOut)
	}

	explorer := read(t, root, "dist/opencode/agents/aid-explorer.md")
	for _, want := range []string{"mode: subagent\npermission:\n  edit: deny\n---\n", "You are the explorer."} {
		if !strings.Contains(explorer, want) {
			t.Errorf("opencode read-only agent missing %q:\n%s", want, explorer)
		}
	}
	if mech := read(t, root, "dist/opencode/agents/aid-mechanic.md"); strings.Contains(mech, "permission:") {
		t.Errorf("writing agent got a permission block:\n%s", mech)
	}

	toml := read(t, root, "dist/codex/agents/aid-explorer.toml")
	if !strings.Contains(toml, "sandbox_mode = \"read-only\"\ndeveloper_instructions = '''\nYou are the explorer. See aid-orchestrate.\n'''\n") {
		t.Errorf("codex read-only agent:\n%s", toml)
	}
	if mech := read(t, root, "dist/codex/agents/aid-mechanic.toml"); strings.Contains(mech, "sandbox_mode") ||
		!strings.Contains(mech, `description = "Builds what's specified."`) {
		t.Errorf("codex writing agent:\n%s", mech)
	}

	orch := read(t, root, "dist/opencode/agents/aid-orchestrator.md")
	if !strings.Contains(orch, "mode: primary\n---\n") || !strings.HasSuffix(orch, "Delegate to aid-explorer.\n") ||
		strings.Contains(orch, "user-invocable") {
		t.Errorf("orchestrator:\n%s", orch)
	}
	if !strings.Contains(orch, `<html> & quotes \"x\"`) {
		t.Errorf("description should be JSON-quoted without HTML escaping:\n%s", orch)
	}

	var agents struct {
		Agents []struct {
			Name         string   `json:"name"`
			Model        string   `json:"model"`
			Capabilities []string `json:"capabilities"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(read(t, root, "agents-manifest.json")), &agents); err != nil {
		t.Fatal(err)
	}
	if len(agents.Agents) != 2 || agents.Agents[1].Model != "sonnet" ||
		strings.Join(agents.Agents[1].Capabilities, ",") != "implement,test" {
		t.Errorf("agents manifest: %+v", agents)
	}
	if skills := read(t, root, "skills-manifest.json"); !strings.Contains(skills, `"loop": "think"`) {
		t.Errorf("skills manifest:\n%s", skills)
	}
}

func TestOrphans(t *testing.T) {
	root := fixture(t)
	compile(root, false)
	write(t, root, "dist/codex/agents/aid-gone.toml", "old")
	if code, _, errOut := compile(root, true); code != 1 || !strings.Contains(errOut, "aid-gone.toml (orphan)") {
		t.Fatalf("check: %d %q", code, errOut)
	}
	compile(root, false)
	if _, err := os.Stat(filepath.Join(root, "dist/codex/agents/aid-gone.toml")); !os.IsNotExist(err) {
		t.Fatalf("build left the orphan: %v", err)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct {
		name, path, content, want string
	}{
		{"tier", "agents/aid-explorer.md", strings.Replace(explorerAgent, "model: haiku", "model: haiku\ntier: low", 1), `no "tier"`},
		{"bad model", "agents/aid-explorer.md", strings.Replace(explorerAgent, "model: haiku", "model: gpt", 1), "model must be one of"},
		{"bad loop", "agents/aid-explorer.md", strings.Replace(explorerAgent, "loop: think", "loop: build", 1), "loop must be one of"},
		{"readOnly type", "agents/aid-explorer.md", strings.Replace(explorerAgent, "readOnly: true", "readOnly: yes", 1), "readOnly must be true or false"},
		{"readOnly needs Write+Edit", "agents/aid-explorer.md", strings.Replace(explorerAgent, "Write, Edit, ", "", 1), "must list Write and Edit"},
		{"toml delimiter", "agents/aid-explorer.md", explorerAgent + "'''\n", "may not contain '''"},
		{"agent name", "agents/aid-explorer.md", strings.Replace(explorerAgent, "name: aid-explorer", "name: aid-scout", 1), "must equal filename"},
		{"agent prefix", "agents/explorer.md", strings.Replace(explorerAgent, "name: aid-explorer", "name: explorer", 1), `carry the "aid-" prefix`},
		{"orchestrator agent", "agents/aid-orchestrator.md", strings.Replace(explorerAgent, "name: aid-explorer", "name: aid-orchestrator", 1), "never a subagent"},
		{"missing frontmatter", "agents/aid-explorer.md", "no frontmatter\n", "missing frontmatter"},
		{"unsupported line", "agents/aid-explorer.md", strings.Replace(explorerAgent, "loop: think", "loop: think\n{bad}", 1), "unsupported frontmatter line"},
		{"user-invocable", "skills/aid-think-explore/SKILL.md", strings.Replace(thinkSkill, "user-invocable: false\n", "", 1), "user-invocable: false required"},
		{"user-invocable true", "skills/aid-think-explore/SKILL.md", strings.Replace(thinkSkill, "user-invocable: false", "user-invocable: true", 1), "user-invocable: false required"},
		{"no loop", "skills/aid-think-explore/SKILL.md", strings.Replace(thinkSkill, "**Loop:** think", "", 1), "must declare **Loop:**"},
		{"stage loop mismatch", "skills/aid-think-explore/SKILL.md", strings.Replace(thinkSkill, "**Loop:** think", "**Loop:** make", 1), "stage skill of the think loop declares **Loop:** make"},
		{"skill name", "skills/aid-think-explore/SKILL.md", strings.Replace(thinkSkill, "name: aid-think-explore", "name: aid-explore", 1), "must equal directory"},
		{"description required", "skills/aid-think-explore/SKILL.md", strings.Replace(thinkSkill, "description: Explore stage of the think loop.\n", "", 1), "description required"},
		{"description length", "skills/aid-think-explore/SKILL.md", strings.Replace(thinkSkill, "Explore stage", strings.Repeat("x", 1025), 1), "over 1024 chars"},
		{"unknown reference", "skills/aid-think-explore/SKILL.md", thinkSkill + "Then aid-think-wander.\n", `unknown skill/agent "aid-think-wander"`},
		{"missing skill file", "skills/aid-empty/notes.md", "x", "skills/aid-empty/SKILL.md: missing"},
		{"config unknown key", "config/harness.json", `{"prefix": "aid-", "plugn": "x"}`, `unknown field "plugn"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fixture(t)
			write(t, root, c.path, c.content)
			code, _, errOut := compile(root, false)
			if code != 1 || !strings.Contains(errOut, c.want) {
				t.Fatalf("want exit 1 with %q, got %d:\n%s", c.want, code, errOut)
			}
			if _, err := os.Stat(filepath.Join(root, "skills-manifest.json")); !os.IsNotExist(err) {
				t.Fatal("an invalid project must not be rendered")
			}
		})
	}
}

func TestMissingOrchestrateSkill(t *testing.T) {
	root := fixture(t)
	if err := os.RemoveAll(filepath.Join(root, "skills", "aid-orchestrate")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "agents/aid-explorer.md", strings.Replace(explorerAgent, " See aid-orchestrate.", "", 1))
	if code, _, errOut := compile(root, false); code != 1 || !strings.Contains(errOut, "OpenCode orchestrator is built from it") {
		t.Fatalf("got %d %q", code, errOut)
	}
}

func TestEveryErrorReported(t *testing.T) {
	root := fixture(t)
	write(t, root, "agents/aid-explorer.md", strings.Replace(explorerAgent, "model: haiku", "model: gpt\ntier: low", 1))
	_, _, errOut := compile(root, false)
	for _, want := range []string{"model must be one of", `no "tier"`} {
		if !strings.Contains(errOut, want) {
			t.Errorf("missing %q in:\n%s", want, errOut)
		}
	}
}
