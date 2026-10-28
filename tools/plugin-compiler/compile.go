package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

var (
	loops  = []string{"think", "make", "learn", "all"}
	models = []string{"haiku", "sonnet", "opus", "fable", "inherit"}
	loopRe = regexp.MustCompile(`(?m)^\*\*Loop:\*\* (\w+)`)
)

const compiler = "tools/plugin-compiler"

type config struct {
	Comment  string `json:"//"`
	Prefix   string `json:"prefix"`
	Plugin   string `json:"plugin"`
	OpenCode struct {
		ReadOnlyPermission map[string]string `json:"readOnlyPermission"`
		Orchestrator       struct {
			Name  string `json:"name"`
			Skill string `json:"skill"`
		} `json:"orchestrator"`
	} `json:"opencode"`
	Codex struct {
		ReadOnlySandbox string `json:"readOnlySandbox"`
	} `json:"codex"`
}

type skill struct {
	Name, Description, Loop, Path, Body string
}

type agent struct {
	Name, Role, Description, Loop, Model string
	ReadOnly                             bool
	Capabilities                         []string
	Path, Body                           string
}

type project struct {
	cfg    config
	skills []skill
	agents []agent
}

// output is one generated file, by path relative to the root.
type output struct {
	path, content string
}

func loadConfig(root string) (config, error) {
	var cfg config
	data, err := os.ReadFile(filepath.Join(root, "config", "harness.json"))
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("config/harness.json: %w", err)
	}
	if cfg.Prefix == "" || cfg.Plugin == "" || cfg.OpenCode.Orchestrator.Name == "" ||
		cfg.OpenCode.Orchestrator.Skill == "" || cfg.Codex.ReadOnlySandbox == "" {
		return cfg, fmt.Errorf("config/harness.json: prefix, plugin, opencode.orchestrator.{name,skill} and codex.readOnlySandbox are required")
	}
	return cfg, nil
}

// load reads and validates every skill and agent. It returns every problem
// found, so one run reports them all.
func load(root string) (*project, []string) {
	cfg, err := loadConfig(root)
	if err != nil {
		return nil, []string{err.Error()}
	}
	p := &project{cfg: cfg}
	var errs []string
	p.loadSkills(root, &errs)
	p.loadAgents(root, &errs)
	p.checkReferences(&errs)
	return p, errs
}

func (p *project) loadSkills(root string, errs *[]string) {
	stageRe := regexp.MustCompile(`^` + regexp.QuoteMeta(p.cfg.Prefix) + `(think|make|learn)-[a-z][a-z-]*$`)
	entries, err := os.ReadDir(filepath.Join(root, "skills"))
	if err != nil {
		*errs = append(*errs, "skills: "+err.Error())
		return
	}
	for _, e := range entries { // ReadDir sorts by name
		if !e.IsDir() {
			continue
		}
		dir := e.Name()
		file := "skills/" + dir + "/SKILL.md"
		text, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			*errs = append(*errs, file+": missing")
			continue
		}
		data, body := parseFrontmatter(string(text), file, errs)
		if name := str(data["name"]); name != dir {
			*errs = append(*errs, fmt.Sprintf("%s: name %q must equal directory %q", file, name, dir))
		}
		if !strings.HasPrefix(dir, p.cfg.Prefix) {
			*errs = append(*errs, fmt.Sprintf("%s: skill names carry the %q prefix", file, p.cfg.Prefix))
		}
		desc := str(data["description"])
		if desc == "" {
			*errs = append(*errs, file+": description required")
		} else if len([]rune(desc)) > 1024 {
			*errs = append(*errs, file+": description over 1024 chars")
		}
		if v, ok := data["user-invocable"].(bool); !ok || v {
			*errs = append(*errs, file+": user-invocable: false required (no skill is a user command)")
		}
		loop := ""
		if m := loopRe.FindStringSubmatch(body); m != nil {
			loop = strings.ToLower(m[1])
		}
		if !slices.Contains(loops, loop) {
			*errs = append(*errs, fmt.Sprintf("%s: body must declare **Loop:** %s", file, strings.Join(loops, "|")))
		}
		if m := stageRe.FindStringSubmatch(dir); m != nil && m[1] != loop {
			*errs = append(*errs, fmt.Sprintf("%s: stage skill of the %s loop declares **Loop:** %s", file, m[1], loop))
		}
		p.skills = append(p.skills, skill{Name: dir, Description: desc, Loop: loop, Path: file, Body: body})
	}
}

func (p *project) loadAgents(root string, errs *[]string) {
	entries, err := os.ReadDir(filepath.Join(root, "agents"))
	if err != nil {
		*errs = append(*errs, "agents: "+err.Error())
		return
	}
	orch := p.cfg.OpenCode.Orchestrator
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		file := "agents/" + e.Name()
		text, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			*errs = append(*errs, file+": "+err.Error())
			continue
		}
		data, body := parseFrontmatter(string(text), file, errs)
		name := strings.TrimSuffix(e.Name(), ".md")
		if got := str(data["name"]); got != name {
			*errs = append(*errs, fmt.Sprintf("%s: name %q must equal filename", file, got))
		}
		if !strings.HasPrefix(name, p.cfg.Prefix) {
			*errs = append(*errs, fmt.Sprintf("%s: agent names carry the %q prefix", file, p.cfg.Prefix))
		}
		desc := str(data["description"])
		if desc == "" {
			*errs = append(*errs, file+": description required")
		}
		loop, model := str(data["loop"]), str(data["model"])
		if !slices.Contains(loops, loop) {
			*errs = append(*errs, fmt.Sprintf("%s: loop must be one of %s", file, strings.Join(loops, "|")))
		}
		if !slices.Contains(models, model) {
			*errs = append(*errs, fmt.Sprintf("%s: model must be one of %s", file, strings.Join(models, "|")))
		}
		if _, ok := data["tier"]; ok {
			*errs = append(*errs, file+`: no "tier", agents declare a Claude model`)
		}
		if name == orch.Name {
			*errs = append(*errs, fmt.Sprintf("%s: the orchestrator is never a subagent; it's generated from skills/%s", file, orch.Skill))
		}
		readOnly, ok := data["readOnly"].(bool)
		if !ok {
			*errs = append(*errs, file+": readOnly must be true or false")
		}
		disallowed := asList(data["disallowedTools"])
		if readOnly && !(slices.Contains(disallowed, "Write") && slices.Contains(disallowed, "Edit")) {
			*errs = append(*errs, file+": readOnly agents must list Write and Edit in disallowedTools (Claude enforcement)")
		}
		if strings.Contains(body, "'''") {
			*errs = append(*errs, file+": body may not contain ''' (Codex TOML literal delimiter)")
		}
		p.agents = append(p.agents, agent{
			Name:         name,
			Role:         strings.TrimPrefix(name, p.cfg.Prefix),
			Description:  desc,
			Loop:         loop,
			Model:        model,
			ReadOnly:     readOnly,
			Capabilities: asList(data["capabilities"]),
			Path:         file,
			Body:         strings.TrimSpace(body) + "\n",
		})
	}
}

// checkReferences requires every aid-* token in a body to name a real skill or
// agent. Catches renames.
func (p *project) checkReferences(errs *[]string) {
	orch := p.cfg.OpenCode.Orchestrator
	if p.orchestrate() == nil {
		*errs = append(*errs, fmt.Sprintf("skills/%s: missing (the OpenCode orchestrator is built from it)", orch.Skill))
	}
	known := map[string]bool{orch.Name: true}
	for _, s := range p.skills {
		known[s.Name] = true
	}
	for _, a := range p.agents {
		known[a.Name] = true
	}
	refRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(p.cfg.Prefix) + `[a-z][a-z-]*[a-z]\b`)
	check := func(path, body string) {
		for _, token := range refRe.FindAllString(body, -1) {
			if !known[token] {
				*errs = append(*errs, fmt.Sprintf("%s: references unknown skill/agent %q", path, token))
			}
		}
	}
	for _, s := range p.skills {
		check(s.Path, s.Body)
	}
	for _, a := range p.agents {
		check(a.Path, a.Body)
	}
}

func (p *project) orchestrate() *skill {
	for i := range p.skills {
		if p.skills[i].Name == p.cfg.OpenCode.Orchestrator.Skill {
			return &p.skills[i]
		}
	}
	return nil
}

// render produces every generated file. It assumes load reported no errors.
func render(p *project) []output {
	banner := "Generated by " + compiler + " from agents/ — do not edit."
	var outs []output

	type skillEntry struct {
		Name        string `json:"name"`
		Loop        string `json:"loop"`
		Description string `json:"description"`
		Path        string `json:"path"`
	}
	skills := []skillEntry{}
	for _, s := range p.skills {
		skills = append(skills, skillEntry{s.Name, s.Loop, s.Description, s.Path})
	}
	outs = append(outs, output{"skills-manifest.json", jsonDoc(struct {
		Comment string       `json:"//"`
		Plugin  string       `json:"plugin"`
		Skills  []skillEntry `json:"skills"`
	}{"Generated by " + compiler + " from skills/*/SKILL.md — do not edit.", p.cfg.Plugin, skills})})

	type agentEntry struct {
		Name         string   `json:"name"`
		Role         string   `json:"role"`
		Loop         string   `json:"loop"`
		Model        string   `json:"model"`
		ReadOnly     bool     `json:"readOnly"`
		Capabilities []string `json:"capabilities"`
		Description  string   `json:"description"`
		Path         string   `json:"path"`
	}
	agents := []agentEntry{}
	for _, a := range p.agents {
		agents = append(agents, agentEntry{a.Name, a.Role, a.Loop, a.Model, a.ReadOnly, a.Capabilities, a.Description, a.Path})
	}
	outs = append(outs, output{"agents-manifest.json", jsonDoc(struct {
		Comment string       `json:"//"`
		Plugin  string       `json:"plugin"`
		Agents  []agentEntry `json:"agents"`
	}{banner + " The harness-neutral agent registry, read by the OpenCode entry and the guards.", p.cfg.Plugin, agents})})

	perms := make([]string, 0, len(p.cfg.OpenCode.ReadOnlyPermission))
	for k := range p.cfg.OpenCode.ReadOnlyPermission {
		perms = append(perms, k)
	}
	sort.Strings(perms)

	for _, a := range p.agents {
		fm := []string{"---", "description: " + jsonString(a.Description), "mode: subagent"}
		if a.ReadOnly {
			fm = append(fm, "permission:")
			for _, k := range perms {
				fm = append(fm, fmt.Sprintf("  %s: %s", k, p.cfg.OpenCode.ReadOnlyPermission[k]))
			}
		}
		fm = append(fm, "---", "<!-- "+banner+" -->", "")
		outs = append(outs, output{"dist/opencode/agents/" + a.Name + ".md", strings.Join(fm, "\n") + a.Body})

		toml := []string{"# " + banner, "name = " + jsonString(a.Name), "description = " + jsonString(a.Description)}
		if a.ReadOnly {
			toml = append(toml, "sandbox_mode = "+jsonString(p.cfg.Codex.ReadOnlySandbox))
		}
		toml = append(toml, "developer_instructions = '''\n"+a.Body+"'''")
		outs = append(outs, output{"dist/codex/agents/" + a.Name + ".toml", strings.Join(toml, "\n") + "\n"})
	}

	orch := p.orchestrate()
	outs = append(outs, output{"dist/opencode/agents/" + p.cfg.OpenCode.Orchestrator.Name + ".md", strings.Join([]string{
		"---",
		"description: " + jsonString(orch.Description),
		"mode: primary",
		"---",
		"<!-- Generated by " + compiler + " from " + orch.Path + " — do not edit. -->",
		"",
	}, "\n") + strings.TrimSpace(orch.Body) + "\n"})
	return outs
}

// generatedDirs hold only generated files; anything else in them is an orphan.
var generatedDirs = []string{"dist/opencode/agents", "dist/codex/agents"}

// sync writes outs under root and removes orphans, or with check only reports
// what differs. It returns the stale paths.
func sync(root string, outs []output, check bool) ([]string, error) {
	var stale []string
	want := map[string]bool{}
	for _, o := range outs {
		want[o.path] = true
		abs := filepath.Join(root, filepath.FromSlash(o.path))
		current, err := os.ReadFile(abs)
		if err == nil && string(current) == o.content {
			continue
		}
		if check {
			stale = append(stale, o.path)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, []byte(o.content), 0o644); err != nil {
			return nil, err
		}
	}
	for _, dir := range generatedDirs {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		for _, e := range entries {
			path := dir + "/" + e.Name()
			if want[path] {
				continue
			}
			if check {
				stale = append(stale, path+" (orphan)")
			} else if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				return nil, err
			}
		}
	}
	return stale, nil
}

// jsonString quotes s as a JSON string (also valid YAML and TOML basic strings).
func jsonString(s string) string {
	return strings.TrimSuffix(encode(s, ""), "\n")
}

// jsonDoc is v as two-space indented JSON with a trailing newline.
func jsonDoc(v any) string {
	return encode(v, "  ")
}

func encode(v any, indent string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		panic(err) // only strings and fixed structs are encoded
	}
	return b.String()
}
