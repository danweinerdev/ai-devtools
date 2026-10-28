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

// Project config is `<cwd>/.claude/aid.json`:
//
//	{"models": {"<agent>": "<model>"},
//	 "guards": {"inline_budget": 20, "exempt_verification": true},
//	 "sensitive": {"rules": [{"name": "...", "patterns": [...], "paths": [...],
//	                          "allow_agents": [...]}]}}
//
// A missing file is an empty config (guards use defaults). An invalid one fails
// loudly: Load returns a *ConfigError, and Run turns that into a deny that says
// what to fix. Only a bug in a guard's own code fails open.

var (
	// ConfigPath is the project config, relative to the session's directory.
	ConfigPath = filepath.Join(".claude", "aid.json")

	models        = []string{"haiku", "sonnet", "opus", "fable"}
	topKeys       = []string{"models", "guards", "sensitive"}
	guardKeys     = []string{"inline_budget", "exempt_verification"}
	sensitiveKeys = []string{"rules"}
	ruleKeys      = []string{"name", "patterns", "paths", "allow_agents"}
)

const (
	orchestrator  = "aid-orchestrator"
	defaultBudget = 20
)

var genericAgents = []string{"general-purpose", "explore", "plan", "claude"}

// Config is a validated aid.json.
type Config struct {
	Models             map[string]string
	InlineBudget       int
	ExemptVerification bool
	Sensitive          []Rule // nil: the gate isn't configured
	HasSensitive       bool
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

func defaults() Config {
	return Config{Models: map[string]string{}, InlineBudget: defaultBudget, ExemptVerification: true}
}

// Load reads and validates `<cwd>/.claude/aid.json`. A missing file is the
// default config.
func Load(cwd string) (Config, error) {
	raw, err := os.ReadFile(filepath.Join(cwd, ConfigPath))
	if errors.Is(err, fs.ErrNotExist) {
		return defaults(), nil
	}
	if err != nil {
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = pe.Err
		}
		return Config{}, &ConfigError{ConfigPath, []string{fmt.Sprintf("cannot be read (%v)", err)}}
	}
	data, err := parse(raw)
	if err != nil {
		return Config{}, &ConfigError{ConfigPath, []string{fmt.Sprintf("is not valid JSON (%v)", err)}}
	}
	cfg, problems := validate(data)
	if len(problems) > 0 {
		return Config{}, &ConfigError{ConfigPath, problems}
	}
	return cfg, nil
}

// validate returns the config and every problem in it, as readable lines.
func validate(data any) (Config, []string) {
	cfg := defaults()
	top, ok := data.(*Object)
	if !ok {
		return cfg, []string{"the top level must be a JSON object"}
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	unknown(top, topKeys, "top level", &problems)

	if top.Has("models") {
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
