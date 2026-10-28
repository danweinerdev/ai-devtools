package guard

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJSON writes data (a string verbatim, anything else as JSON) at path.
func writeJSON(t *testing.T, path string, data any) {
	t.Helper()
	raw, ok := data.(string)
	if !ok {
		b, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(b)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) ocProject(data any) {
	writeJSON(f.t, filepath.Join(f.cwd, ".opencode", "aid.json"), data)
}

// ocGlobal writes the global config under XDG_CONFIG_HOME when set, else ~/.config.
func (f *fixture) ocGlobal(data any) string {
	base := f.vars["XDG_CONFIG_HOME"]
	if base == "" {
		base = filepath.Join(f.env.Home, ".config")
	}
	path := filepath.Join(base, "opencode", "aid.json")
	writeJSON(f.t, path, data)
	return path
}

func routing(profile, model string, agents ...string) m {
	as := m{}
	for _, a := range agents {
		as[a] = profile
	}
	return m{"profiles": m{profile: m{"model": model}}, "agents": as}
}

func problemsOf(t *testing.T, err error) string {
	t.Helper()
	var ce *ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("want a ConfigError, got %v", err)
	}
	return strings.Join(ce.Problems, "\n")
}

func TestOpenCodeConfigPrecedence(t *testing.T) {
	f := newFixture(t)
	cfg, err := LoadOpenCode(f.cwd, f.env)
	if err != nil || cfg.Source != "" || len(cfg.Profiles) != 0 || len(cfg.Agents) != 0 || cfg.Path != ".opencode/aid.json" {
		t.Fatalf("no file: want empty defaults, got %+v %v", cfg, err)
	}

	// The global file under ~/.config is used when the project has none.
	home := f.ocGlobal(routing("g", "anthropic/claude-haiku-5-5", "aid-explorer"))
	if cfg, err = LoadOpenCode(f.cwd, f.env); err != nil || cfg.Agents["aid-explorer"] != "g" || cfg.Source != home {
		t.Fatalf("global: got %+v %v", cfg, err)
	}

	// XDG_CONFIG_HOME wins over ~/.config.
	f.vars["XDG_CONFIG_HOME"] = t.TempDir()
	xdg := f.ocGlobal(routing("x", "openai/gpt-x", "aid-oracle"))
	if cfg, err = LoadOpenCode(f.cwd, f.env); err != nil || cfg.Agents["aid-oracle"] != "x" || cfg.Source != xdg {
		t.Fatalf("XDG: got %+v %v", cfg, err)
	}

	// The project file wins, whole: no merging with the global one.
	f.ocProject(routing("p", "anthropic/claude-opus-5-5", "aid-mechanic"))
	cfg, err = LoadOpenCode(f.cwd, f.env)
	if err != nil || cfg.Agents["aid-mechanic"] != "p" || len(cfg.Agents) != 1 || !strings.HasSuffix(cfg.Source, filepath.Join(".opencode", "aid.json")) {
		t.Fatalf("project: got %+v %v", cfg, err)
	}
}

func TestOpenCodeConfigInvalidIsLoud(t *testing.T) {
	f := newFixture(t)
	f.ocGlobal(routing("g", "anthropic/claude-haiku-5-5", "aid-explorer"))

	// An invalid project file is an error, not a fall-through to the valid global.
	f.ocProject("{broken")
	if _, err := LoadOpenCode(f.cwd, f.env); !strings.Contains(problemsOf(t, err), "is not valid JSON") {
		t.Fatalf("got %v", err)
	}

	// An invalid global file is an error too, naming it.
	g := newFixture(t)
	path := g.ocGlobal(m{"profiles": m{"g": m{"model": "nope"}}})
	_, err := LoadOpenCode(g.cwd, g.env)
	var ce *ConfigError
	if !errors.As(err, &ce) || ce.Path != filepath.ToSlash(path) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenCodeConfigValidation(t *testing.T) {
	cases := []struct {
		name string
		data any
		want string // "" means valid
	}{
		{"variant", m{"profiles": m{"a": m{"model": "anthropic/claude-opus-5-5#high"}}}, ""},
		{"gateway model with slashes", m{"profiles": m{"a": m{"model": "openrouter/anthropic/claude-opus"}}}, ""},
		{"effort", m{"profiles": m{"a": m{"model": "openai/gpt-x", "reasoning_effort": "xhigh"}}}, ""},
		{"hide free models", m{"hide_free_models": true}, ""},
		{"no provider", m{"profiles": m{"a": m{"model": "claude"}}}, "profiles.a.model: `claude` must be provider/model"},
		{"empty variant", m{"profiles": m{"a": m{"model": "a/b#"}}}, "profiles.a.model"},
		{"two variants", m{"profiles": m{"a": m{"model": "a/b#c#d"}}}, "profiles.a.model"},
		{"missing model", m{"profiles": m{"a": m{}}}, "profiles.a.model"},
		{"bad effort", m{"profiles": m{"a": m{"model": "a/b", "reasoning_effort": "max"}}}, "not one of none, minimal, low, medium, high, xhigh"},
		{"variant and effort", m{"profiles": m{"a": m{"model": "a/b#c", "reasoning_effort": "high"}}}, "not both"},
		{"profile key typo", m{"profiles": m{"a": m{"model": "a/b", "reasoning_efort": "high"}}}, "did you mean `reasoning_effort`"},
		{"profile name", m{"profiles": m{"Big": m{"model": "a/b"}}}, "profiles.Big: profile names are lowercase"},
		{"missing profile", m{"profiles": m{"fast": m{"model": "a/b"}}, "agents": m{"aid-explorer": "fsat"}}, "profile `fsat` doesn't exist (did you mean `fast`?)"},
		{"agent value", m{"agents": m{"aid-explorer": 3}}, "agents.aid-explorer: must name a profile"},
		{"hide type", m{"hide_free_models": "yes"}, "hide_free_models: must be true or false"},
		{"claude key", m{"models": m{"aid-explorer": "haiku"}}, "unknown key `models`"},
		{"guards shared", m{"guards": m{"inline_budget": -1}}, "guards.inline_budget"},
		{"sensitive RE2", m{"sensitive": m{"rules": []any{m{"name": "x", "patterns": []any{"(?<=a)b"}}}}}, "RE2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			f.ocProject(c.data)
			_, err := LoadOpenCode(f.cwd, f.env)
			if c.want == "" {
				if err != nil {
					t.Fatalf("want valid, got %v", err)
				}
				return
			}
			if p := problemsOf(t, err); !strings.Contains(p, c.want) {
				t.Fatalf("want %q in:\n%s", c.want, p)
			}
		})
	}

	// A profile with problems of its own is still a profile agents can name.
	f := newFixture(t)
	f.ocProject(m{"profiles": m{"a": m{"model": "bad"}}, "agents": m{"aid-oracle": "a"}})
	if p := problemsOf(t, func() error { _, err := LoadOpenCode(f.cwd, f.env); return err }()); strings.Contains(p, "doesn't exist") {
		t.Fatalf("a present profile was reported missing:\n%s", p)
	}

	// The routing keys aren't Claude Code's.
	c := newFixture(t)
	c.config(m{"profiles": m{}})
	if _, err := Load(c.cwd); !strings.Contains(problemsOf(t, err), "unknown key `profiles`") {
		t.Fatalf("got %v", err)
	}
}

func TestOpenCodeConfigParsesProfiles(t *testing.T) {
	f := newFixture(t)
	f.ocProject(m{"profiles": m{
		"deep": m{"model": "openrouter/anthropic/claude-opus#thinking"},
		"fast": m{"model": "anthropic/claude-haiku-5-5", "reasoning_effort": "low"},
	}})
	cfg, err := LoadOpenCode(f.cwd, f.env)
	if err != nil {
		t.Fatal(err)
	}
	if d := cfg.Profiles["deep"]; d.Provider != "openrouter" || d.ID != "anthropic/claude-opus" || d.Variant != "thinking" {
		t.Fatalf("deep: %+v", d)
	}
	if f := cfg.Profiles["fast"]; f.Provider != "anthropic" || f.ID != "claude-haiku-5-5" || f.Variant != "" || f.ReasoningEffort != "low" {
		t.Fatalf("fast: %+v", f)
	}
}

// ocCall runs the OpenCode tool guard on one call.
func (f *fixture) ocCall(tool string, args m, extra ...any) (Verdict, error) {
	f.t.Helper()
	p := m{"tool": tool, "args": args, "sessionID": "s1", "agent": "aid-orchestrator", "cwd": f.cwd}
	for i := 0; i+1 < len(extra); i += 2 {
		p[extra[i].(string)] = extra[i+1]
	}
	raw, err := json.Marshal(p)
	if err != nil {
		f.t.Fatal(err)
	}
	return OpenCodeTool(raw, f.env)
}

func wantVerdict(t *testing.T, v Verdict, err error, decision, reason string) {
	t.Helper()
	if err != nil {
		t.Fatalf("aid failed: %v", err)
	}
	if v.Decision != decision || !strings.Contains(v.Reason, reason) {
		t.Fatalf("got %+v, want %s containing %q", v, decision, reason)
	}
}

func TestOpenCodeToolGeneric(t *testing.T) {
	f := newFixture(t)
	for agent, reason := range map[string]string{
		"general":          "general is blocked",
		"explore":          "Use aid-explorer",
		"plan":             "plan is a primary agent",
		"build":            "build is a primary agent",
		"title":            "internal agent",
		"aid-orchestrator": "never a subagent",
		"":                 "need an agent",
	} {
		v, err := f.ocCall("subagent", m{"agent": agent, "prompt": "p"})
		wantVerdict(t, v, err, "deny", reason)
	}
	for _, agent := range []string{"aid-explorer", "aid-oracle", "project-reviewer"} {
		v, err := f.ocCall("subagent", m{"agent": agent, "prompt": "p"})
		wantVerdict(t, v, err, "allow", "")
	}
	// Tools no guard covers pass, whatever their arguments.
	v, err := f.ocCall("webfetch", m{"url": "https://example.com"})
	wantVerdict(t, v, err, "allow", "")
}

func TestOpenCodeToolSensitive(t *testing.T) {
	f := newFixture(t)
	f.ocProject(m{"sensitive": m{"rules": []any{m{"name": "pii", "patterns": []any{`SECRET-\d+`},
		"allow_agents": []any{"aid-oracle"}}}}})
	v, err := f.ocCall("subagent", m{"agent": "aid-explorer", "prompt": "see SECRET-42"})
	wantVerdict(t, v, err, "deny", "Rules live in .opencode/aid.json")
	v, err = f.ocCall("subagent", m{"agent": "aid-oracle", "prompt": "see SECRET-42"})
	wantVerdict(t, v, err, "allow", "")
	// Any argument counts, not just the prompt.
	v, err = f.ocCall("subagent", m{"agent": "aid-explorer", "prompt": "p", "description": "SECRET-7"})
	wantVerdict(t, v, err, "deny", "pii")
}

func TestOpenCodeToolInlineBudget(t *testing.T) {
	f := newFixture(t)
	f.ocProject(m{"guards": m{"inline_budget": 2}})
	search := func(extra ...any) (Verdict, error) { return f.ocCall("grep", m{"pattern": "x"}, extra...) }

	for range 2 {
		v, err := search()
		wantVerdict(t, v, err, "allow", "")
	}
	v, err := search()
	wantVerdict(t, v, err, "deny", "one subagent call")
	if !strings.Contains(v.Reason, ".opencode/aid.json") {
		t.Fatalf("the budget message should name the OpenCode config: %q", v.Reason)
	}

	// A subagent's own calls are never throttled: the roster, OpenCode's
	// generic subagents, or whatever agent_mode says.
	for _, extra := range [][]any{{"agent", "aid-explorer"}, {"agent", "general"}, {"agent", "reviewer", "agent_mode", "subagent"}} {
		v, err = search(extra...)
		wantVerdict(t, v, err, "allow", "")
	}
	// agent_mode primary throttles even a roster name.
	v, err = search("agent", "aid-explorer", "agent_mode", "primary")
	wantVerdict(t, v, err, "deny", "exceeds the budget")

	// Verification, the OpenCode config itself, and aid calls aren't counted.
	for tool, args := range map[string]m{
		"shell": {"command": "go test ./..."},
		"read":  {"filePath": filepath.Join(f.cwd, ".opencode", "aid.json")},
	} {
		v, err = f.ocCall(tool, args)
		wantVerdict(t, v, err, "allow", "")
	}
	v, err = f.ocCall("shell", m{"command": "aid ready"})
	wantVerdict(t, v, err, "allow", "")

	// A foreground subagent dispatch resets the budget; a background one doesn't.
	v, err = f.ocCall("subagent", m{"agent": "aid-explorer", "prompt": "p", "background": true})
	wantVerdict(t, v, err, "allow", "")
	v, err = search()
	wantVerdict(t, v, err, "deny", "exceeds the budget")
	v, err = f.ocCall("subagent", m{"agent": "aid-explorer", "prompt": "p"})
	wantVerdict(t, v, err, "allow", "")
	v, err = search()
	wantVerdict(t, v, err, "allow", "")

	// The counter is per session.
	g := newFixture(t)
	g.ocProject(m{"guards": m{"inline_budget": 1}})
	g.env.TempDir = f.env.TempDir
	v, err = g.ocCall("glob", m{"pattern": "**/*.go"}, "sessionID", "other")
	wantVerdict(t, v, err, "allow", "")
}

func TestOpenCodeToolInvalidConfigDenies(t *testing.T) {
	f := newFixture(t)
	f.ocProject(m{"profiles": m{"fast": m{"model": "nope"}}})
	v, err := f.ocCall("subagent", m{"agent": "aid-explorer", "prompt": "p"})
	wantVerdict(t, v, err, "deny", "profiles.fast.model")
	if !strings.Contains(v.Reason, ".opencode/aid.json is invalid") {
		t.Fatalf("the deny should say what to fix: %q", v.Reason)
	}
	// The broken file can still be read to fix it.
	v, err = f.ocCall("read", m{"filePath": filepath.Join(f.cwd, ".opencode", "aid.json")})
	wantVerdict(t, v, err, "allow", "")
}

func TestOpenCodeToolOffAndFailures(t *testing.T) {
	f := newFixture(t)
	f.vars["AID_GUARDS"] = "off"
	v, err := f.ocCall("subagent", m{"agent": "general"})
	wantVerdict(t, v, err, "allow", "")
	delete(f.vars, "AID_GUARDS")

	// Unusable input is aid's own failure: an error, so the plugin fails open.
	if _, err := OpenCodeTool([]byte("not json"), f.env); err == nil {
		t.Fatal("unusable stdin should be an error")
	}

	// A guard bug fails open: an error, never a deny.
	saved := openCodeGuards
	t.Cleanup(func() { openCodeGuards = saved })
	openCodeGuards = []func(*Object, Env) (Decision, error){func(*Object, Env) (Decision, error) { panic("boom") }}
	if v, err := f.ocCall("subagent", m{"agent": "aid-explorer"}); err == nil || v.Decision == "deny" {
		t.Fatalf("a panicking guard should be an error, got %+v %v", v, err)
	}
}

func TestPolicyPerHarness(t *testing.T) {
	oc := Policy(OpenCode)
	for _, want := range []string{"general ->", "explore -> aid-explorer", ".opencode/aid.json", "subagent tool"} {
		if !strings.Contains(oc, want) {
			t.Errorf("OpenCode policy lacks %q", want)
		}
	}
	if strings.Contains(oc, ".claude") || strings.Contains(oc, "general-purpose") {
		t.Error("OpenCode policy names Claude Code's files or built-ins")
	}
	if !strings.Contains(Policy(Claude), "general-purpose") {
		t.Error("Claude policy changed")
	}
}
