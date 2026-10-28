// Package guard holds the Claude Code guardrail hooks that aid runs as
// `aid guard <name>`: each reads the hook payload, decides, and returns the
// exit code and output Claude Code acts on. Exit 2 with a reason on stderr
// denies; exit 0 allows. An invalid .claude/aid.json denies with what to fix;
// a bug in a guard's own code fails open, so a guard defect never wedges a
// session; AID_GUARDS=off disables the guards for the session.
package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// Decision is what a guard tells Claude Code.
type Decision struct {
	Exit   int
	Stdout string
	Stderr string
}

// Env is what a guard reads from its surroundings, injected so tests control it.
type Env struct {
	Getenv  func(string) string
	Getwd   func() (string, error)
	TempDir string // where the inline budget keeps its counters
	Home    string // for expanding ~ in paths
	// Harness picks the config file the guards read and the wording of their
	// messages; empty is Claude Code.
	Harness Harness
}

// OSEnv is the real environment.
func OSEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Getenv: os.Getenv, Getwd: os.Getwd, TempDir: os.TempDir(), Home: home}
}

// loadConfig reads the config of env's harness.
func loadConfig(cwd string, env Env) (Config, error) {
	if env.Harness == OpenCode {
		return LoadOpenCode(cwd, env)
	}
	return Load(cwd)
}

// spawnCall names a delegation call in env's harness, for messages.
func spawnCall(env Env) string {
	if env.Harness == OpenCode {
		return "subagent"
	}
	return "Agent"
}

var agentTools = []string{"Agent", "Task"}

// Names lists the guards, in hooks.json order.
var Names = []string{"session-start", "inline", "generic", "sensitive", "models"}

// guards maps a name to its decision function. A nil payload means stdin
// wasn't a JSON object.
var guards = map[string]func(p *Object, env Env) (Decision, error){
	"inline":    inline,
	"generic":   generic,
	"sensitive": sensitive,
	"models":    modelOverride,
}

// Run runs the named guard on the raw hook payload.
func Run(name string, stdin []byte, env Env) (d Decision) {
	if name == "session-start" {
		return sessionStart(payloadOf(stdin), env)
	}
	g, ok := guards[name]
	if !ok {
		return Decision{Exit: 1, Stderr: fmt.Sprintf("aid-guards: no guard %q (have %s)\n", name, strings.Join(Names, ", "))}
	}
	if strings.ToLower(strings.TrimSpace(env.Getenv("AID_GUARDS"))) == "off" {
		return Decision{}
	}
	defer func() {
		if r := recover(); r != nil {
			d = Decision{Stderr: fmt.Sprintf("aid-guards: guard %s errored (%v); allowing (fail open).\n", name, r)}
		}
	}()
	d, err := g(payloadOf(stdin), env)
	var ce *ConfigError
	if errors.As(err, &ce) {
		return Decision{Exit: 2, Stderr: "aid-guards: " + ce.Error() + "\n"}
	}
	if err != nil {
		panic(err) // any other error is a guard bug: fail open, above
	}
	return d
}

// payloadOf is the hook payload, or nil when stdin isn't a JSON object.
func payloadOf(stdin []byte) *Object {
	v, err := parse(stdin)
	if err != nil {
		return nil
	}
	o, _ := v.(*Object)
	return o
}

func toolInput(p *Object) *Object {
	if o, ok := p.Get("tool_input").(*Object); ok {
		return o
	}
	return &Object{Values: map[string]any{}}
}

func isAgentTool(p *Object) bool {
	return slices.Contains(agentTools, str(p.Get("tool_name")))
}

// cwdOf is the session's directory from the payload, else the process's.
func cwdOf(p *Object, env Env) string {
	if s := str(p.Get("cwd")); s != "" {
		return s
	}
	wd, _ := env.Getwd()
	return wd
}

func deny(msg string) Decision {
	return Decision{Exit: 2, Stderr: "aid-guards: " + msg + "\n"}
}

// --- generic: no generic delegation, no orchestrator subagent -------------

const roles = "aid-mechanic for well-specified building, aid-oracle for high-risk " +
	"work or deep review, aid-explorer for research, aid-designer for " +
	"visual/UX, aid-creator for first-pass creative work, aid-craftsman " +
	"for refining a working first pass"

func generic(p *Object, _ Env) (Decision, error) {
	if p == nil || !isAgentTool(p) {
		return Decision{}, nil
	}
	name := Bare(toolInput(p).Get("subagent_type"))
	if name != orchestrator && name != "" && !slices.Contains(genericAgents, strings.ToLower(name)) {
		return Decision{}, nil
	}
	switch key := strings.ToLower(name); {
	case name == orchestrator:
		return deny("aid-orchestrator is the main conversation, never a subagent. Do its work in the main loop."), nil
	case key == "explore":
		return deny("Explore is blocked. Use aid-explorer for read-only research."), nil
	case key == "plan":
		return deny("Plan is blocked. Stay in the main loop and use the aid-think and aid-make-breakdown skills."), nil
	case key == "claude":
		return deny("claude is blocked (generic). Pick the matching aid-* role: " + roles + "."), nil
	case key == "general-purpose":
		return deny("general-purpose is blocked. Pick the matching aid-* role: " + roles + "."), nil
	}
	return deny("Agent calls need a subagent_type. Pick the matching aid-* role: " + roles + "."), nil
}

// --- models: apply aid.json model overrides on spawn ------------------------

// modelOverride rewrites the spawn's model through updatedInput, which
// replaces the whole input, so every original field is copied. Claude Code
// needs a permissionDecision beside updatedInput; "allow" is used.
func modelOverride(p *Object, env Env) (Decision, error) {
	if p == nil || !isAgentTool(p) {
		return Decision{}, nil
	}
	in := toolInput(p)
	name := Bare(in.Get("subagent_type"))
	if name == "" || name == orchestrator || slices.Contains(genericAgents, strings.ToLower(name)) {
		return Decision{}, nil
	}
	cfg, err := loadConfig(cwdOf(p, env), env)
	if err != nil {
		return Decision{}, err
	}
	model := cfg.Models[name]
	if model == "" || in.Get("model") == model {
		return Decision{}, nil
	}
	updated := &Object{Keys: slices.Clone(in.Keys), Values: map[string]any{}}
	for k, v := range in.Values {
		updated.Values[k] = v
	}
	updated.Set("model", model)
	out, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "allow",
		"permissionDecisionReason": fmt.Sprintf("aid.json model override for %s: %s", name, model),
		"updatedInput":             updated,
	}})
	if err != nil {
		return Decision{}, err
	}
	return Decision{Stdout: string(out) + "\n"}, nil
}

// --- sensitive: opt-in sensitivity gate --------------------------------------

// sensitive denies a spawn whose text matches a rule's pattern (a regex over
// the spawn's strings) or path (a glob over its whitespace-separated tokens)
// unless the target agent is in the rule's allow_agents.
func sensitive(p *Object, env Env) (Decision, error) {
	if p == nil || !isAgentTool(p) {
		return Decision{}, nil
	}
	cfg, err := loadConfig(cwdOf(p, env), env)
	if err != nil {
		return Decision{}, err
	}
	if !cfg.HasSensitive {
		return Decision{}, nil // opt-in
	}
	in := toolInput(p)
	target := Bare(in.Get("subagent_type"))
	var strs []string
	collectStrings(in, &strs)
	text := strings.Join(strs, "\n")
	tokens := strings.Fields(text)
	for _, rule := range cfg.Sensitive {
		hit := slices.ContainsFunc(rule.Patterns, func(re *regexp.Regexp) bool { return re.MatchString(text) })
		if !hit {
			for _, g := range rule.Paths {
				if slices.ContainsFunc(tokens, func(tok string) bool { return fnmatch(tok, g) }) {
					hit = true
					break
				}
			}
		}
		allowed := slices.ContainsFunc(rule.AllowAgents, func(a string) bool { return Bare(a) == target })
		if hit && !allowed {
			who := target
			if who == "" {
				who = "no agent"
			}
			return deny(fmt.Sprintf("sensitivity rule '%s' matched content in this spawn and '%s' is not in "+
				"its allow_agents. Handle this data inline in the main loop. (Rules live in %s.)",
				rule.Name, who, cfg.Path)), nil
		}
	}
	return Decision{}, nil
}

func collectStrings(v any, out *[]string) {
	switch v := v.(type) {
	case string:
		*out = append(*out, v)
	case *Object:
		for _, k := range v.Keys {
			collectStrings(v.Values[k], out)
		}
	case []any:
		for _, x := range v {
			collectStrings(x, out)
		}
	}
}
