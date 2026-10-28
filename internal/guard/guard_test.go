package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture is a session: a project directory, a temp dir for counters, and an
// environment the guards read.
type fixture struct {
	t    *testing.T
	cwd  string
	env  Env
	vars map[string]string
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, cwd: t.TempDir(), vars: map[string]string{}}
	f.env = Env{
		Getenv:  func(k string) string { return f.vars[k] },
		Getwd:   func() (string, error) { return f.cwd, nil },
		TempDir: t.TempDir(),
		Home:    t.TempDir(),
	}
	return f
}

// config writes .claude/aid.json: a string verbatim, anything else as JSON.
func (f *fixture) config(data any) {
	f.t.Helper()
	raw, ok := data.(string)
	if !ok {
		b, err := json.Marshal(data)
		if err != nil {
			f.t.Fatal(err)
		}
		raw = string(b)
	}
	if err := os.MkdirAll(filepath.Join(f.cwd, ".claude"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cwd, ".claude", "aid.json"), []byte(raw), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) run(name string, payload any) Decision {
	f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	return Run(name, raw, f.env)
}

func (f *fixture) raw(name, stdin string) Decision { return Run(name, []byte(stdin), f.env) }

type m = map[string]any

// spawn is an Agent call; a nil type leaves subagent_type out.
func (f *fixture) spawn(subagentType any, extra ...any) m {
	ti := m{}
	if subagentType != nil {
		ti["subagent_type"] = subagentType
	}
	for i := 0; i+1 < len(extra); i += 2 {
		ti[extra[i].(string)] = extra[i+1]
	}
	return m{"tool_name": "Agent", "tool_input": ti, "cwd": f.cwd}
}

func wantExit(t *testing.T, d Decision, code int) {
	t.Helper()
	if d.Exit != code {
		t.Fatalf("exit %d, want %d; stderr %q stdout %q", d.Exit, code, d.Stderr, d.Stdout)
	}
}

func contains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("want %q in:\n%s", want, got)
	}
}

// --- generic ----------------------------------------------------------------

func TestGenericDenied(t *testing.T) {
	f := newFixture(t)
	for _, c := range []struct {
		name any
		hint string
	}{
		{"general-purpose", "aid-mechanic"}, {"Explore", "aid-explorer"},
		{"Plan", "aid-make-breakdown"}, {"claude", "aid-oracle"},
		{nil, "aid-explorer"}, {"", "aid-explorer"},
		{"aid-orchestrator", "main conversation"},
		{"ai-development:aid-orchestrator", "main conversation"},
		{"plugin:general-purpose", "aid-mechanic"},
	} {
		d := f.run("generic", f.spawn(c.name))
		wantExit(t, d, 2)
		contains(t, d.Stderr, c.hint)
	}
}

func TestGenericTaskToolNameAlsoGuarded(t *testing.T) {
	f := newFixture(t)
	p := f.spawn("general-purpose")
	p["tool_name"] = "Task"
	wantExit(t, f.run("generic", p), 2)
}

func TestGenericRosterAndProjectAgentsAllowed(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"aid-oracle", "aid-explorer", "ai-development:aid-explorer",
		"ai-development:aid-oracle", "my-project-agent"} {
		wantExit(t, f.run("generic", f.spawn(name)), 0)
	}
}

func TestGenericModelsFileDoesNotUnblockGeneric(t *testing.T) {
	f := newFixture(t)
	f.config(m{"models": m{"general-purpose": "haiku"}})
	wantExit(t, f.run("generic", f.spawn("general-purpose")), 2)
	// The override itself is invalid config, so models denies too; it never rewrites.
	if d := f.run("models", f.spawn("general-purpose")); d.Stdout != "" {
		t.Fatalf("models rewrote a generic spawn: %q", d.Stdout)
	}
}

func TestGenericEscapeHatchAndFailOpen(t *testing.T) {
	f := newFixture(t)
	f.vars["AID_GUARDS"] = "off"
	wantExit(t, f.run("generic", f.spawn("general-purpose")), 0)
	f.vars["AID_GUARDS"] = " OFF "
	wantExit(t, f.run("generic", f.spawn("general-purpose")), 0)
	delete(f.vars, "AID_GUARDS")
	for _, raw := range []string{"not json", "", "null", "[1]"} {
		wantExit(t, f.raw("generic", raw), 0)
	}
}

func TestGuardBugFailsOpen(t *testing.T) {
	guards["boom"] = func(*Object, Env) (Decision, error) { panic("division by zero") }
	defer delete(guards, "boom")
	d := Run("boom", []byte(`{}`), newFixture(t).env)
	wantExit(t, d, 0)
	contains(t, d.Stderr, "fail open")
}

// --- models -----------------------------------------------------------------

func overrideOf(t *testing.T, d Decision) map[string]any {
	t.Helper()
	var out struct {
		HookSpecificOutput map[string]any `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(d.Stdout), &out); err != nil {
		t.Fatalf("stdout %q: %v", d.Stdout, err)
	}
	return out.HookSpecificOutput
}

func TestModelsRewritesWithAllowAndKeepsFields(t *testing.T) {
	f := newFixture(t)
	f.config(m{"models": m{"aid-explorer": "sonnet"}})
	d := f.run("models", f.spawn("aid-explorer", "prompt", "hi", "model", "haiku"))
	wantExit(t, d, 0)
	out := overrideOf(t, d)
	if out["hookEventName"] != "PreToolUse" || out["permissionDecision"] != "allow" {
		t.Fatalf("got %v", out)
	}
	want := map[string]any{"subagent_type": "aid-explorer", "prompt": "hi", "model": "sonnet"}
	got := out["updatedInput"].(map[string]any)
	if len(got) != len(want) || got["subagent_type"] != want["subagent_type"] || got["prompt"] != "hi" || got["model"] != "sonnet" {
		t.Fatalf("updatedInput %v", got)
	}
}

func TestModelsKeepsInputKeyOrder(t *testing.T) {
	f := newFixture(t)
	f.config(m{"models": m{"aid-explorer": "sonnet"}})
	d := f.raw("models", `{"tool_name": "Agent", "cwd": "`+filepath.ToSlash(f.cwd)+`",
		"tool_input": {"z": 1, "subagent_type": "aid-explorer", "a": {"y": 2, "b": 3}}}`)
	contains(t, d.Stdout, `"updatedInput":{"z":1,"subagent_type":"aid-explorer","a":{"y":2,"b":3},"model":"sonnet"}`)
}

func TestModelsNamespacedNameMatchesBareKey(t *testing.T) {
	f := newFixture(t)
	f.config(m{"models": m{"aid-explorer": "sonnet"}})
	in := overrideOf(t, f.run("models", f.spawn("ai-development:aid-explorer", "prompt", "x")))["updatedInput"].(map[string]any)
	if in["model"] != "sonnet" || in["subagent_type"] != "ai-development:aid-explorer" {
		t.Fatalf("got %v", in)
	}
}

func TestModelsProjectAgentOverride(t *testing.T) {
	f := newFixture(t)
	f.config(m{"models": m{"reviewer": "opus"}})
	in := overrideOf(t, f.run("models", f.spawn("reviewer")))["updatedInput"].(map[string]any)
	if in["model"] != "opus" {
		t.Fatalf("got %v", in)
	}
}

func TestModelsNoChangeWhenUnlistedMissingConfigOrAlreadySet(t *testing.T) {
	f := newFixture(t)
	if d := f.run("models", f.spawn("aid-explorer")); d.Stdout != "" {
		t.Fatal("rewrote without a config")
	}
	f.config(m{"models": m{"aid-explorer": "sonnet"}})
	if d := f.run("models", f.spawn("aid-oracle")); d.Stdout != "" {
		t.Fatal("rewrote an unlisted agent")
	}
	if d := f.run("models", f.spawn("aid-explorer", "model", "sonnet")); d.Stdout != "" {
		t.Fatal("rewrote a spawn already on the model")
	}
}

func TestModelsBadConfigDeniesAndOff(t *testing.T) {
	f := newFixture(t)
	f.config("{broken")
	d := f.run("models", f.spawn("aid-explorer"))
	if d.Exit != 2 || d.Stdout != "" {
		t.Fatalf("got %+v", d)
	}
	contains(t, d.Stderr, "not valid JSON")
	f.config(m{"models": m{"aid-explorer": "sonnet"}})
	f.vars["AID_GUARDS"] = "off"
	if d := f.run("models", f.spawn("aid-explorer")); d.Stdout != "" {
		t.Fatal("AID_GUARDS=off still rewrote")
	}
}

// --- sensitive ----------------------------------------------------------------

var piiRule = m{"rules": []any{m{"name": "pii", "patterns": []any{`SECRET-\d+`}, "paths": []any{"*.pem"},
	"allow_agents": []any{"aid-oracle"}}}}

func TestSensitiveOffWithoutConfig(t *testing.T) {
	f := newFixture(t)
	wantExit(t, f.run("sensitive", f.spawn("aid-explorer", "prompt", "SECRET-1")), 0)
}

func TestSensitivePatternAndPathMatchDeny(t *testing.T) {
	f := newFixture(t)
	f.config(m{"sensitive": piiRule})
	for _, prompt := range []string{"see SECRET-42", "read key.pem", "read keys/dir/key.pem"} {
		d := f.run("sensitive", f.spawn("aid-explorer", "prompt", prompt))
		wantExit(t, d, 2)
		contains(t, d.Stderr, "pii")
	}
	// Strings anywhere in the input count, not only the prompt.
	wantExit(t, f.run("sensitive", f.spawn("aid-explorer", "nested", m{"deep": []any{"SECRET-7"}})), 2)
}

func TestSensitiveAllowAgentsBareAndNamespaced(t *testing.T) {
	f := newFixture(t)
	f.config(m{"sensitive": piiRule})
	for _, name := range []string{"aid-oracle", "ai-development:aid-oracle"} {
		wantExit(t, f.run("sensitive", f.spawn(name, "prompt", "SECRET-1")), 0)
	}
}

func TestSensitiveNoMatchAllowsAndOffSwitch(t *testing.T) {
	f := newFixture(t)
	f.config(m{"sensitive": piiRule})
	wantExit(t, f.run("sensitive", f.spawn("aid-explorer", "prompt", "clean")), 0)
	f.vars["AID_GUARDS"] = "off"
	wantExit(t, f.run("sensitive", f.spawn("aid-explorer", "prompt", "SECRET-1")), 0)
}

func TestSensitiveMalformedRulesAndBrokenFileDeny(t *testing.T) {
	for _, bad := range []any{
		m{"sensitive": "nope"},
		m{"sensitive": []any{m{"name": "x", "patterns": []any{"a"}}}},
		m{"sensitive": m{"rules": []any{m{"name": "x", "patterns": []any{"("}}}}},
		m{"sensitive": m{"rules": []any{m{"patterns": []any{"a"}}}}},
		m{"sensitive": m{"rules": []any{m{"name": "x"}}}},
		"{broken",
	} {
		f := newFixture(t)
		f.config(bad)
		d := f.run("sensitive", f.spawn("aid-oracle", "prompt", "x"))
		wantExit(t, d, 2)
		contains(t, d.Stderr, "is invalid")
	}
}

func TestSensitiveRejectsPatternsRE2CannotCompile(t *testing.T) {
	f := newFixture(t)
	// Valid PCRE, but RE2 has no lookaround: refused, and the message says why.
	f.config(m{"sensitive": m{"rules": []any{m{"name": "x", "patterns": []any{`(?<=a)b`}}}}})
	d := f.run("sensitive", f.spawn("aid-oracle"))
	wantExit(t, d, 2)
	contains(t, d.Stderr, "RE2 syntax")
}

func TestFnmatch(t *testing.T) {
	for _, c := range []struct {
		name, pattern string
		want          bool
	}{
		{"key.pem", "*.pem", true}, {"a/b/key.pem", "*.pem", true}, {"key.pem.bak", "*.pem", false},
		{"secrets/x/y", "secrets/**", true}, {"secret", "secret?", false}, {"secret1", "secret?", true},
		{"a1", "a[0-9]", true}, {"ab", "a[!0-9]", true}, {"a1", "a[!0-9]", false},
		{"a[", "a[", true}, {"a.b", "a.b", true}, {"axb", "a.b", false}, {"Ünï.pem", "*.pem", true},
	} {
		if got := fnmatch(c.name, c.pattern); got != c.want {
			t.Errorf("fnmatch(%q, %q) = %v, want %v", c.name, c.pattern, got, c.want)
		}
	}
}

// --- config -----------------------------------------------------------------

func denyMessage(t *testing.T, data any) string {
	t.Helper()
	f := newFixture(t)
	f.config(data)
	d := f.run("models", f.spawn("aid-explorer"))
	wantExit(t, d, 2)
	return d.Stderr
}

func TestConfigUnknownKeysGetDidYouMean(t *testing.T) {
	contains(t, denyMessage(t, m{"model": m{}}), "did you mean `models`")
	contains(t, denyMessage(t, m{"guards": m{"inline_budge": 5}}), "did you mean `inline_budget`")
	contains(t, denyMessage(t, m{"sensitive": m{"rules": []any{m{"name": "x", "paths": []any{"*.pem"}, "allow_agent": []any{}}}}}),
		"did you mean `allow_agents`")
	if msg := denyMessage(t, m{"zzzzzz": 1}); strings.Contains(msg, "did you mean") {
		t.Fatalf("suggested a far-off key: %s", msg)
	}
}

func TestConfigBadValuesAreNamed(t *testing.T) {
	for _, c := range []struct {
		data   any
		needle string
	}{
		{m{"models": m{"aid-explorer": "gpt-5"}}, "models.aid-explorer"},
		{m{"models": m{"general-purpose": "opus"}}, "no effect"},
		{m{"models": []any{}}, "models: must be an object"},
		{m{"guards": m{"inline_budget": "40"}}, "guards.inline_budget"},
		{m{"guards": m{"inline_budget": true}}, "guards.inline_budget"},
		{m{"guards": m{"inline_budget": 1.5}}, "guards.inline_budget"},
		{m{"guards": m{"exempt_verification": "yes"}}, "guards.exempt_verification"},
		{[]any{}, "top level must be a JSON object"},
	} {
		contains(t, denyMessage(t, c.data), c.needle)
	}
}

func TestConfigEveryProblemReportedAtOnce(t *testing.T) {
	msg := denyMessage(t, m{"models": m{"aid-explorer": "gpt"}, "guards": m{"inline_budget": -1}})
	contains(t, msg, "models.aid-explorer")
	contains(t, msg, "guards.inline_budget")
}

func TestConfigInlineGuardDeniesButConfigFileStaysReadable(t *testing.T) {
	f := newFixture(t)
	f.config("{broken")
	p := m{"session_id": "s", "prompt_id": "p", "cwd": f.cwd, "tool_name": "Grep", "tool_input": m{"pattern": "x"}}
	wantExit(t, f.run("inline", p), 2)
	p["tool_name"], p["tool_input"] = "Read", m{"file_path": ".claude/aid.json"}
	wantExit(t, f.run("inline", p), 0)
}

func TestConfigSessionStartReportsErrors(t *testing.T) {
	f := newFixture(t)
	f.config(m{"models": m{"aid-explorer": "gpt-5"}})
	d := f.run("session-start", m{"cwd": f.cwd})
	wantExit(t, d, 0)
	contains(t, d.Stdout, "Configuration error")
	contains(t, d.Stdout, "models.aid-explorer")
	f.config(m{"models": m{"aid-explorer": "sonnet"}})
	if d := f.run("session-start", m{"cwd": f.cwd}); strings.Contains(d.Stdout, "Configuration error") {
		t.Fatal("reported an error for a valid config")
	}
}

// --- inline -----------------------------------------------------------------

type inlineFixture struct {
	*fixture
	session string
}

func newInline(t *testing.T) *inlineFixture {
	f := &inlineFixture{fixture: newFixture(t), session: t.Name()}
	f.config(m{"guards": m{"inline_budget": 3}})
	return f
}

func (f *inlineFixture) call(tool string, ti m) Decision {
	return f.run("inline", m{"session_id": f.session, "prompt_id": "p", "cwd": f.cwd, "tool_name": tool, "tool_input": ti})
}

func (f *inlineFixture) grep() Decision { return f.call("Grep", m{"pattern": "x"}) }

func (f *inlineFixture) exhaust() {
	for range 3 {
		wantExit(f.t, f.grep(), 0)
	}
}

func TestInlineBlocksAfterBudgetPointingAtExplorer(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	d := f.grep()
	wantExit(t, d, 2)
	contains(t, d.Stderr, "aid-explorer")
	contains(t, d.Stderr, `{"guards": {"inline_budget": 20}}`)
}

func TestInlineForegroundAgentResetsBackgroundDoesNot(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	f.grep()
	f.call("Agent", m{"subagent_type": "aid-explorer", "run_in_background": true})
	wantExit(t, f.grep(), 2)
	f.call("Agent", m{"subagent_type": "aid-explorer"}) // absent counts as background
	wantExit(t, f.grep(), 2)
	f.call("Agent", m{"subagent_type": "aid-explorer", "run_in_background": false})
	wantExit(t, f.grep(), 0)
}

func TestInlineSubagentCallsNeverCounted(t *testing.T) {
	f := newInline(t)
	p := m{"session_id": f.session, "prompt_id": "p", "cwd": f.cwd, "agent_id": "a1", "tool_name": "Grep", "tool_input": m{}}
	for range 6 {
		wantExit(t, f.run("inline", p), 0)
	}
}

func TestInlinePRDAndAidDirReadsExempt(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	wantExit(t, f.call("Read", m{"file_path": filepath.Join(f.cwd, "docs", "PRDs", "X.md")}), 0)
	wantExit(t, f.call("Read", m{"file_path": filepath.Join(f.cwd, ".aid", "tasks.json")}), 0)
	wantExit(t, f.call("Glob", m{"pattern": "docs/PRDs/**/*.md"}), 0)
	wantExit(t, f.call("Read", m{"file_path": filepath.Join(f.cwd, "src", "a.py")}), 2)
	// A look-alike sibling directory isn't under the exempt one.
	wantExit(t, f.call("Read", m{"file_path": filepath.Join(f.cwd, "docs", "PRDsX", "a.md")}), 2)
}

func TestInlineMixedPathsStillCount(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	if err := os.MkdirAll(filepath.Join(f.cwd, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantExit(t, f.call("Bash", m{"command": "grep -r foo docs/PRDs src"}), 2)
	// A Unicode path token is still a path, so it can't slip past the exemption.
	wantExit(t, f.call("Bash", m{"command": "cat docs/PRDs/a.md src/Ünï.go"}), 2)
}

func TestInlineAidCLIExempt(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	wantExit(t, f.call("Bash", m{"command": "aid graph list | head"}), 0)
	wantExit(t, f.call("Bash", m{"command": "aid graph list; grep -r foo src"}), 2)
}

func TestInlineVerificationExempt(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	for _, cmd := range []string{"git log --oneline", "pytest -q", "ps aux | wc -l", "make check",
		"go test ./...", "grep -c foo file", "ps -ef | grep -c aid", "cat /proc/meminfo"} {
		wantExit(t, f.call("Bash", m{"command": cmd}), 0)
	}
	for _, cmd := range []string{"grep -r foo src", "git log -p | head -500", "ps -ef | grep aid",
		"date; grep -r secret ."} {
		wantExit(t, f.call("Bash", m{"command": cmd}), 2)
	}
}

func TestInlineVerificationNotExemptWhenTurnedOff(t *testing.T) {
	f := newInline(t)
	f.config(m{"guards": m{"inline_budget": 3, "exempt_verification": false}})
	f.exhaust()
	wantExit(t, f.call("Bash", m{"command": "git log --oneline"}), 0) // not searchy: never counted
	wantExit(t, f.call("Bash", m{"command": "ls | wc -l"}), 2)
}

func TestInlineWritesAreNotExploration(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	wantExit(t, f.call("Bash", m{"command": "cat a > b"}), 0)
	wantExit(t, f.call("Bash", m{"command": "grep -r x src &> out"}), 0)
	wantExit(t, f.call("Bash", m{"command": "grep -r x src 2>/dev/null"}), 2)
	wantExit(t, f.call("Bash", m{"command": "grep -r x src 2>&1"}), 2)
}

func TestInlineOffAndFailOpen(t *testing.T) {
	f := newInline(t)
	f.exhaust()
	f.vars["AID_GUARDS"] = "off"
	wantExit(t, f.grep(), 0)
	delete(f.vars, "AID_GUARDS")
	wantExit(t, f.raw("inline", "garbage"), 0)
}

func TestInlineDefaultBudgetWithoutConfig(t *testing.T) {
	f := newInline(t)
	if err := os.Remove(filepath.Join(f.cwd, ".claude", "aid.json")); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		wantExit(t, f.grep(), 0)
	}
	wantExit(t, f.grep(), 2)
}

func TestInlineRegexTranslations(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"cat a > b", true}, {"cat a >> b", true}, {"x &> y", true},
		{"cmd 2>/dev/null", false}, {"cmd 2>&1", false}, {"cmd 2>>err", true}, {"ls", false},
	} {
		if got := writeRedirect(c.cmd); got != c.want {
			t.Errorf("writeRedirect(%q) = %v", c.cmd, got)
		}
	}
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"ls | head", true}, {"du -sh * | tail -3", true}, {"ps | grep x", true},
		{"ps | grep -c x", false}, {"ps | grep -l x", false}, {"ps | rg -i -c x", false},
		{"ps -ef | grep -c python", false}, {"ls | wc -l", false}, {"ls | header", false},
	} {
		if got := paged(c.cmd); got != c.want {
			t.Errorf("paged(%q) = %v", c.cmd, got)
		}
	}
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"grep -c x f", true}, {"grep -rl x .", true}, {"rg -n -L x", true}, {"grep -r x .", false},
		{"cd x && grep -c y f", true}, {"grepper -c", false},
	} {
		if got := grepCounting.MatchString(c.cmd); got != c.want {
			t.Errorf("grepCounting(%q) = %v", c.cmd, got)
		}
	}
}

func TestGlobLiteralPrefix(t *testing.T) {
	for pattern, want := range map[string]string{
		".plans/**/*.md": ".plans", "/abs/Specs/*.md": "/abs/Specs", "**/*.py": "", "": "",
		"docs/PRDs/x.md": "docs/PRDs/x.md", "/": "",
	} {
		if got := globLiteralPrefix(pattern); got != want {
			t.Errorf("globLiteralPrefix(%q) = %q, want %q", pattern, got, want)
		}
	}
}

// --- session start ------------------------------------------------------------

func TestSessionStartPrintsPolicyWithoutSkillAndWithSkill(t *testing.T) {
	f := newFixture(t)
	plugin := t.TempDir()
	f.vars["CLAUDE_PLUGIN_ROOT"] = plugin
	d := f.run("session-start", m{})
	wantExit(t, d, 0)
	contains(t, d.Stdout, "aid-explorer")
	contains(t, strings.ToLower(d.Stdout), "project agents")
	dir := filepath.Join(plugin, "skills", "aid-orchestrate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: aid-orchestrate\n---\nORCH-BODY\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := f.run("session-start", m{}).Stdout
	if !strings.HasPrefix(out, "ORCH-BODY") || strings.Contains(out, "name: aid-orchestrate") ||
		strings.Index(out, "ORCH-BODY") > strings.Index(out, "Routing policy") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestSessionStartNeverMentionsPRDIndexAndIgnoresGuardsOff(t *testing.T) {
	f := newFixture(t)
	f.vars["AID_GUARDS"] = "off"
	d := f.raw("session-start", "not json")
	wantExit(t, d, 0)
	contains(t, d.Stdout, "Routing policy")
	if strings.Contains(d.Stdout, "PRD index") {
		t.Fatal("mentions a PRD index")
	}
}

func TestUnknownGuard(t *testing.T) {
	d := Run("nope", nil, newFixture(t).env)
	wantExit(t, d, 1)
	contains(t, d.Stderr, "session-start")
}
