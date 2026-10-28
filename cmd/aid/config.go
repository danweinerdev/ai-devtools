package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/danweinerdev/ai-devtools/internal/guard"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, configCmd) }

// openCodeView and claudeView are a validated aid.json as `aid config --json`
// prints it. Source is null when no file exists; the maps are then empty.
type openCodeView struct {
	Source         *string                  `json:"source"`
	Profiles       map[string]guard.Profile `json:"profiles"`
	Agents         map[string]string        `json:"agents"`
	HideFreeModels bool                     `json:"hide_free_models"`
}

type claudeView struct {
	Source *string           `json:"source"`
	Models map[string]string `json:"models"`
}

func configCmd(a *app) *cobra.Command {
	var harness string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print a harness's validated aid.json: model routing, budgets, sensitivity rules",
		Long: "Reads and strictly validates the harness's aid.json from the current directory: for opencode, " +
			".opencode/aid.json, else $XDG_CONFIG_HOME/opencode/aid.json (~/.config/opencode/aid.json); for " +
			"claude, .claude/aid.json. With --json it prints {\"source\", \"profiles\", \"agents\", " +
			"\"hide_free_models\"} for opencode or {\"source\", \"models\"} for claude; source is null when no " +
			"file exists. An invalid file exits 1 with every problem on stderr.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := harnessOf(harness)
			if err != nil {
				return err
			}
			env := guard.OSEnv()
			var cfg guard.Config
			if h == guard.OpenCode {
				cfg, err = guard.LoadOpenCode(a.cwd, env)
			} else {
				cfg, err = guard.Load(a.cwd)
			}
			var ce *guard.ConfigError
			if errors.As(err, &ce) {
				_, _ = fmt.Fprintln(a.err, "aid: "+ce.Error())
				return exitCode(1)
			}
			if err != nil {
				return err
			}
			return a.emit(viewOf(h, cfg), configText(h, cfg))
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "opencode", "opencode or claude")
	return cmd
}

// harnessOf reads a --harness flag.
func harnessOf(name string) (guard.Harness, error) {
	switch h := guard.Harness(name); h {
	case guard.Claude, guard.OpenCode:
		return h, nil
	}
	return "", fmt.Errorf("--harness %q: use claude or opencode", name)
}

func viewOf(h guard.Harness, cfg guard.Config) any {
	var source *string
	if cfg.Source != "" {
		source = &cfg.Source
	}
	if h == guard.OpenCode {
		return openCodeView{Source: source, Profiles: cfg.Profiles, Agents: cfg.Agents, HideFreeModels: cfg.HideFreeModels}
	}
	return claudeView{Source: source, Models: cfg.Models}
}

func configText(h guard.Harness, cfg guard.Config) string {
	var b strings.Builder
	if cfg.Source == "" {
		fmt.Fprintf(&b, "no %s: every agent uses the session model and the guards use their defaults\n", cfg.Path)
		return b.String()
	}
	fmt.Fprintf(&b, "source: %s\n", cfg.Source)
	if h == guard.Claude {
		for _, k := range sorted(cfg.Models) {
			fmt.Fprintf(&b, "  %s -> %s\n", k, cfg.Models[k])
		}
		return b.String()
	}
	for _, name := range sorted(cfg.Profiles) {
		p := cfg.Profiles[name]
		effort := ""
		if p.ReasoningEffort != "" {
			effort = " (reasoning " + p.ReasoningEffort + ")"
		}
		fmt.Fprintf(&b, "profile %s: %s%s\n", name, p.Model, effort)
	}
	for _, agent := range sorted(cfg.Agents) {
		fmt.Fprintf(&b, "  %s -> %s\n", agent, cfg.Agents[agent])
	}
	if cfg.HideFreeModels {
		b.WriteString("hide_free_models: true\n")
	}
	return b.String()
}

func sorted[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
