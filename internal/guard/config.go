package guard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Claude Code's project config is `<cwd>/.claude/aid.json`:
//
//	{"models": {"<agent>": "<model>"},
//	 "guards": {"inline_budget": 20, "exempt_verification": true},
//	 "sensitive": {"rules": [{"name": "...", "patterns": [...], "paths": [...],
//	                          "allow_agents": [...]}]}}
//
// OpenCode's is `<cwd>/.opencode/aid.json`, else the global
// `$XDG_CONFIG_HOME/opencode/aid.json` (`~/.config/opencode/aid.json`); the
// first file found is the config, never a merge. It has the same guards and
// sensitive keys, and routes models through named profiles instead:
//
//	{"profiles": {"<name>": {"model": "provider/model[#variant]", "reasoning_effort": "high"}},
//	 "agents": {"<agent>": "<profile>"}, "hide_free_models": true, ...}
//
// An agent missing from agents inherits the session model; nothing has a
// default. A missing file is an empty config (guards use defaults). An invalid
// one fails loudly, even when another file would have been next: the loaders
// return a *ConfigError, and Run turns that into a deny that says what to fix.
// Only a bug in a guard's own code fails open.

var (
	// ConfigPath is Claude Code's project config, relative to the session's
	// directory; OpenCodeConfigPath is OpenCode's.
	ConfigPath         = filepath.Join(".claude", "aid.json")
	OpenCodeConfigPath = filepath.Join(".opencode", "aid.json")

	models        = []string{"haiku", "sonnet", "opus", "fable"}
	claudeKeys    = []string{"models", "guards", "sensitive"}
	openCodeKeys  = []string{"profiles", "agents", "hide_free_models", "guards", "sensitive"}
	profileKeys   = []string{"model", "reasoning_effort"}
	guardKeys     = []string{"inline_budget", "exempt_verification"}
	sensitiveKeys = []string{"rules"}
	ruleKeys      = []string{"name", "patterns", "paths", "allow_agents"}

	// The efforts the model router accepts (its REASONING_EFFORTS).
	efforts     = []string{"none", "minimal", "low", "medium", "high", "xhigh"}
	profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
	// provider/model, the model part free to hold more slashes (gateways such
	// as openrouter/anthropic/x), with an optional #variant.
	modelRef = regexp.MustCompile(`^([^/\s#]+)/([^\s#]+)(?:#([^\s#]+))?$`)
)

// Harness names a config's home: which file is read and which keys it allows.
type Harness string

// The harnesses aid has guards for.
const (
	Claude   Harness = "claude"
	OpenCode Harness = "opencode"
)

const (
	orchestrator  = "aid-orchestrator"
	defaultBudget = 20
)

var genericAgents = []string{"general-purpose", "explore", "plan", "claude"}

// Config is a validated aid.json.
type Config struct {
	// Path names the file in messages: the one read, else where it would go.
	Path string
	// Source is the absolute path of the file read, or "" when there was none.
	Source string

	Models             map[string]string // Claude Code: agent -> model
	Profiles           map[string]Profile
	Agents             map[string]string // OpenCode: agent -> profile
	HideFreeModels     bool
	InlineBudget       int
	ExemptVerification bool
	Sensitive          []Rule // nil: the gate isn't configured
	HasSensitive       bool
}

// Profile is a named OpenCode model choice.
type Profile struct {
	Model           string `json:"model"` // as written: provider/model[#variant]
	Provider        string `json:"provider"`
	ID              string `json:"id"`
	Variant         string `json:"variant,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// Rule is one sensitivity rule.
type Rule struct {
	Name        string
	Patterns    []*regexp.Regexp
	Paths       []string
	AllowAgents []string
}

// ConfigError reports an aid.json that exists but is invalid, with every
// problem found.
type ConfigError struct {
	Path     string
	Problems []string
}

func (e *ConfigError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = "  - " + p
	}
	return fmt.Sprintf("%s is invalid, so the aid guards refuse to run on a partial config. Fix:\n%s\n"+
		"AID_GUARDS=off disables the guards for the session.", e.Path, strings.Join(lines, "\n"))
}

func defaults(path string) Config {
	return Config{Path: path, Models: map[string]string{}, Profiles: map[string]Profile{},
		Agents: map[string]string{}, InlineBudget: defaultBudget, ExemptVerification: true}
}

// Load reads and validates Claude Code's `<cwd>/.claude/aid.json`. A missing
// file is the default config.
func Load(cwd string) (Config, error) {
	return loadFile(Claude, filepath.Join(cwd, ConfigPath), filepath.ToSlash(ConfigPath))
}

// LoadOpenCode reads and validates OpenCode's config: `<cwd>/.opencode/aid.json`
// if it exists, else the global one. Only absence moves on to the next file;
// an invalid file is an error.
func LoadOpenCode(cwd string, env Env) (Config, error) {
	display := filepath.ToSlash(OpenCodeConfigPath)
	project := filepath.Join(cwd, OpenCodeConfigPath)
	if _, err := os.Stat(project); err == nil {
		return loadFile(OpenCode, project, display)
	}
	if global := openCodeGlobal(env); global != "" {
		if _, err := os.Stat(global); err == nil {
			return loadFile(OpenCode, global, filepath.ToSlash(global))
		}
	}
	return defaults(display), nil
}

// openCodeGlobal is `$XDG_CONFIG_HOME/opencode/aid.json`, else the same under
// `~/.config`, or "" with neither.
func openCodeGlobal(env Env) string {
	base := ""
	if env.Getenv != nil {
		base = env.Getenv("XDG_CONFIG_HOME")
	}
	if base == "" && env.Home != "" {
		base = filepath.Join(env.Home, ".config")
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "opencode", "aid.json")
}

// loadFile reads one config file; display names it in messages.
func loadFile(h Harness, path, display string) (Config, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return defaults(display), nil
	}
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return Config{}, &ConfigError{display, []string{fmt.Sprintf("cannot be read (%v)", err)}}
	}
	data, err := parse(raw)
	if err != nil {
		return Config{}, &ConfigError{display, []string{fmt.Sprintf("is not valid JSON (%v)", err)}}
	}
	cfg, problems := validate(h, data)
	if len(problems) > 0 {
		return Config{}, &ConfigError{display, problems}
	}
	cfg.Path = display
	if abs, err := filepath.Abs(path); err == nil {
		cfg.Source = abs
	}
	return cfg, nil
}

// validate returns the config and every problem in it, as readable lines.
func validate(h Harness, data any) (Config, []string) {
	cfg := defaults("")
	top, ok := data.(*Object)
	if !ok {
		return cfg, []string{"the top level must be a JSON object"}
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if h == OpenCode {
		unknown(top, openCodeKeys, "top level", &problems)
		validateRouting(top, &cfg, add, &problems)
	} else {
		unknown(top, claudeKeys, "top level", &problems)
	}

	if h == Claude && top.Has("models") {
		m, ok := top.Get("models").(*Object)
		if !ok {
			add("models: must be an object of agent name -> model")
		} else {
			for _, agent := range m.Keys {
				model := m.Values[agent]
				if strings.TrimSpace(agent) == "" {
					add("models: agent names must be non-empty")
				} else if b := Bare(agent); slices.Contains(genericAgents, strings.ToLower(b)) || b == orchestrator {
					add("models.%s: generic agents and the orchestrator are never spawned, so an override has no effect", agent)
				}
				if s, ok := model.(string); !ok || !slices.Contains(models, s) {
					add("models.%s: `%s` is not one of %s", agent, display(model), strings.Join(models, ", "))
					continue
				}
				cfg.Models[agent] = model.(string)
			}
		}
	}

	if top.Has("guards") {
		g, ok := top.Get("guards").(*Object)
		if !ok {
			add("guards: must be an object")
		} else {
			unknown(g, guardKeys, "guards", &problems)
			if g.Has("inline_budget") {
				if n, ok := integer(g.Get("inline_budget")); !ok || n < 0 {
					add("guards.inline_budget: must be a non-negative integer")
				} else {
					cfg.InlineBudget = n
				}
			}
			if g.Has("exempt_verification") {
				if b, ok := g.Get("exempt_verification").(bool); !ok {
					add("guards.exempt_verification: must be true or false")
				} else {
					cfg.ExemptVerification = b
				}
			}
		}
	}

	if top.Has("sensitive") {
		cfg.HasSensitive = true
		s, ok := top.Get("sensitive").(*Object)
		rules, isList := s.Get("rules").([]any)
		if !ok || !isList {
			add(`sensitive: must be {"rules": [...]}`)
		} else {
			unknown(s, sensitiveKeys, "sensitive", &problems)
			for i, item := range rules {
				where := fmt.Sprintf("sensitive.rules[%d]", i)
				r, ok := item.(*Object)
				if !ok {
					add("%s: must be an object", where)
					continue
				}
				unknown(r, ruleKeys, where, &problems)
				rule := Rule{Name: str(r.Get("name"))}
				if rule.Name == "" {
					add("%s.name: required, a non-empty string", where)
				} else {
					where = fmt.Sprintf("sensitive.rules[%d] (%s)", i, rule.Name)
				}
				patterns := stringList(r, "patterns", where, &problems)
				rule.Paths = stringList(r, "paths", where, &problems)
				rule.AllowAgents = stringList(r, "allow_agents", where, &problems)
				if len(patterns) == 0 && len(rule.Paths) == 0 {
					add("%s: needs at least one pattern or path", where)
				}
				for _, p := range patterns {
					re, err := regexp.Compile(p)
					if err != nil {
						add("%s.patterns: `%s` is not a valid regular expression (RE2 syntax: no lookaround or backreferences; %v)", where, p, err)
						continue
					}
					rule.Patterns = append(rule.Patterns, re)
				}
				cfg.Sensitive = append(cfg.Sensitive, rule)
			}
		}
	}
	return cfg, problems
}

// validateRouting reads OpenCode's profiles, agents and hide_free_models.
func validateRouting(top *Object, cfg *Config, add func(string, ...any), problems *[]string) {
	if top.Has("profiles") {
		ps, ok := top.Get("profiles").(*Object)
		if !ok {
			add("profiles: must be an object of profile name -> {model, reasoning_effort}")
		} else {
			for _, name := range ps.Keys {
				where := "profiles." + name
				if !profileName.MatchString(name) {
					add("%s: profile names are lowercase letters, digits, '.' and '-', starting with a letter or digit", where)
				}
				po, ok := ps.Values[name].(*Object)
				if !ok {
					add("%s: must be an object with a model and optionally a reasoning_effort", where)
					continue
				}
				unknown(po, profileKeys, where, problems)
				prof := Profile{}
				model, isStr := po.Get("model").(string)
				if m := modelRef.FindStringSubmatch(model); !isStr || m == nil {
					add("%s.model: `%s` must be provider/model or provider/model#variant", where, display(po.Get("model")))
				} else {
					prof.Model, prof.Provider, prof.ID, prof.Variant = model, m[1], m[2], m[3]
				}
				if po.Has("reasoning_effort") {
					s, ok := po.Get("reasoning_effort").(string)
					switch {
					case !ok || !slices.Contains(efforts, s):
						add("%s.reasoning_effort: `%s` is not one of %s", where, display(po.Get("reasoning_effort")),
							strings.Join(efforts, ", "))
					case prof.Variant != "":
						add("%s: set a #variant or a reasoning_effort, not both", where)
					default:
						prof.ReasoningEffort = s
					}
				}
				cfg.Profiles[name] = prof
			}
		}
	}
	if top.Has("agents") {
		as, ok := top.Get("agents").(*Object)
		if !ok {
			add("agents: must be an object of agent name -> profile name")
		} else {
			names := profileNames(top)
			for _, agent := range as.Keys {
				profile, isStr := as.Values[agent].(string)
				switch {
				case strings.TrimSpace(agent) == "" || strings.ContainsAny(agent, " \t\n"):
					add("agents: `%s` is not an agent name", agent)
				case !isStr || profile == "":
					add("agents.%s: must name a profile", agent)
				case !slices.Contains(names, profile):
					add("agents.%s: profile `%s` doesn't exist%s", agent, profile, near(profile, names))
				default:
					cfg.Agents[agent] = profile
				}
			}
		}
	}
	if top.Has("hide_free_models") {
		if b, ok := top.Get("hide_free_models").(bool); !ok {
			add("hide_free_models: must be true or false")
		} else {
			cfg.HideFreeModels = b
		}
	}
}

// profileNames are the profile keys as written, valid or not, so a reference
// to a profile with problems of its own reports those, not "doesn't exist".
func profileNames(top *Object) []string {
	if ps, ok := top.Get("profiles").(*Object); ok {
		return ps.Keys
	}
	return nil
}

// near is a did-you-mean suffix, or "".
func near(key string, allowed []string) string {
	if n := closest(key, allowed); n != "" {
		return fmt.Sprintf(" (did you mean `%s`?)", n)
	}
	return ""
}

// unknown reports keys of o not in allowed, with the closest allowed key.
func unknown(o *Object, allowed []string, where string, problems *[]string) {
	for _, key := range o.Keys {
		if slices.Contains(allowed, key) {
			continue
		}
		hint := ""
		if near := closest(key, allowed); near != "" {
			hint = fmt.Sprintf(" (did you mean `%s`?)", near)
		}
		*problems = append(*problems, fmt.Sprintf("%s: unknown key `%s`%s; allowed: %s",
			where, key, hint, strings.Join(allowed, ", ")))
	}
}

// stringList reads o[key] as a list of non-empty strings; absent is empty.
func stringList(o *Object, key, where string, problems *[]string) []string {
	if !o.Has(key) {
		return nil
	}
	list, ok := o.Get(key).([]any)
	out := []string{}
	for _, v := range list {
		if s, isStr := v.(string); isStr && s != "" {
			out = append(out, s)
		} else {
			ok = false
		}
	}
	if !ok {
		*problems = append(*problems, fmt.Sprintf("%s.%s: must be a list of non-empty strings", where, key))
		return nil
	}
	return out
}

// closest is the allowed word most like key, if any scores at least 0.6.
func closest(key string, allowed []string) string {
	best, bestScore := "", 0.6
	for _, a := range allowed {
		if r := ratio(key, a); r >= bestScore && (best == "" || r > bestScore) {
			best, bestScore = a, r
		}
	}
	return best
}

// ratio is difflib.SequenceMatcher's similarity: twice the matched characters
// over the total, matching blocks found by the longest common substring and
// recursing on either side (Ratcliff/Obershelp).
func ratio(a, b string) float64 {
	if len(a)+len(b) == 0 {
		return 1
	}
	return 2 * float64(matched(a, b)) / float64(len(a)+len(b))
}

func matched(a, b string) int {
	bestI, bestJ, bestN := 0, 0, 0
	for i := range a {
		for j := range b {
			n := 0
			for i+n < len(a) && j+n < len(b) && a[i+n] == b[j+n] {
				n++
			}
			if n > bestN {
				bestI, bestJ, bestN = i, j, n
			}
		}
	}
	if bestN == 0 {
		return 0
	}
	return bestN + matched(a[:bestI], b[:bestJ]) + matched(a[bestI+bestN:], b[bestJ+bestN:])
}

// Bare strips a plugin namespace: `ai-development:aid-explorer` is
// `aid-explorer`. A non-string is "".
func Bare(name any) string {
	s, ok := name.(string)
	if !ok {
		return ""
	}
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}
