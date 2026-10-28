package main

import (
	"fmt"
	"io"

	"github.com/danweinerdev/ai-devtools/internal/guard"

	"github.com/spf13/cobra"
)

func init() { commands = append(commands, guardCmd) }

// exitCode is an error carrying only an exit code; the command has already
// written its output.
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("exit %d", int(c)) }

// guardHelp documents each guard for `aid guard <name> --help`.
var guardHelp = map[string]string{
	"session-start": "SessionStart: print the aid-orchestrate skill body (from $CLAUDE_PLUGIN_ROOT), the routing " +
		"policy, and any problems with .claude/aid.json. Always exits 0.",
	"inline": "PreToolUse (Bash|Read|Grep|Glob|Agent|Task): the inline-exploration budget. After " +
		"guards.inline_budget (default 20) search-type calls in one user turn, further ones are denied with a " +
		"pointer to aid-explorer. Verification commands, reads under docs/PRDs/ and .aid/, .claude/aid.json and " +
		"aid CLI calls aren't counted; subagent calls never are; a foreground Agent call resets the count.",
	"generic": "PreToolUse (Agent|Task): deny generic delegation (general-purpose, Explore, Plan, claude, or no " +
		"subagent_type) and the orchestrator, pointing at the matching aid-* role. Roster and project agents pass.",
	"sensitive": "PreToolUse (Agent|Task): the opt-in sensitivity gate. A spawn whose text matches a rule's " +
		"patterns (RE2 regexes) or paths (globs) in .claude/aid.json is denied unless the target agent is in " +
		"the rule's allow_agents.",
	"models": "PreToolUse (Agent|Task): apply .claude/aid.json model overrides by rewriting the spawn's model " +
		"through updatedInput, with permissionDecision allow.",
}

func guardCmd(a *app) *cobra.Command {
	parent := &cobra.Command{
		Use:    "guard <name>",
		Short:  "Claude Code guardrail hooks (run by the plugin's hooks.json)",
		Hidden: true,
		Long: "The Claude Code guardrail hooks. Each reads the hook payload on stdin and exits 0 to allow or 2 " +
			"(reason on stderr) to deny. An invalid .claude/aid.json makes every guard that reads it deny with " +
			"what to fix; a bug in a guard fails open; AID_GUARDS=off disables them for the session. They never " +
			"touch .aid/.",
	}
	for _, name := range guard.Names {
		parent.AddCommand(&cobra.Command{
			Use:   name,
			Short: guardHelp[name],
			Long:  guardHelp[name],
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				stdin, _ := io.ReadAll(cmd.InOrStdin())
				d := guard.Run(name, stdin, guard.OSEnv())
				_, _ = io.WriteString(a.out, d.Stdout)
				_, _ = io.WriteString(a.err, d.Stderr)
				if d.Exit != 0 {
					return exitCode(d.Exit)
				}
				return nil
			},
		})
	}
	return parent
}
