package main

import (
	"encoding/json"
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
	"definition-of-done": "PreToolUse (Bash|Write|Edit|MultiEdit|NotebookEdit): an `aid update-task` call that " +
		"would weaken a task's definition of done (remove or change a check, or change the result) is answered " +
		"\"ask\", so the user approves it; one the guard can't parse that passes --check or --result is asked too. " +
		"File-tool edits of .aid/<work>/tasks.json or state.json are denied: they change only through aid.",
}

// guardEnv is the real environment, with aid's own judge for the
// definition-of-done gate.
func guardEnv() guard.Env {
	env := guard.OSEnv()
	env.DoneGate = doneGate
	return env
}

func guardCmd(a *app) *cobra.Command {
	parent := &cobra.Command{
		Use:    "guard <name>",
		Short:  "Guardrail hooks for Claude Code (hooks.json) and OpenCode (the plugin)",
		Hidden: true,
		Long: "The guardrail hooks. Claude Code's read the hook payload on stdin and exit 0 to allow or 2 " +
			"(reason on stderr) to deny; OpenCode's plugin calls opencode-tool and policy. An invalid aid.json " +
			"makes every guard that reads it deny with what to fix; a bug in a guard fails open; AID_GUARDS=off " +
			"disables them for the session. They never write .aid/; the definition-of-done gate reads the graph.",
	}
	parent.AddCommand(openCodeToolCmd(a), policyCmd(a))
	for _, name := range guard.Names {
		parent.AddCommand(&cobra.Command{
			Use:   name,
			Short: guardHelp[name],
			Long:  guardHelp[name],
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				stdin, _ := io.ReadAll(cmd.InOrStdin())
				d := guard.Run(name, stdin, guardEnv())
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

// openCodeToolCmd is the OpenCode plugin's tool guard. Both decisions exit 0;
// a non-zero exit is aid's own failure, and the plugin then fails open.
func openCodeToolCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "opencode-tool",
		Short: "OpenCode tool guard: decide one tool call for the plugin",
		Long: "Reads {\"tool\", \"args\", \"sessionID\", \"agent\", \"cwd\"} (an OpenCode 2 tool call; an optional " +
			"\"agent_mode\": \"primary\"|\"subagent\" says whether the caller is a subagent) and prints " +
			"{\"decision\": \"allow\"|\"ask\"|\"deny\", \"reason\": \"...\", \"task\": \"...\"}, exit 0 either way. " +
			"It runs the generic, sensitivity and inline-budget guards against .opencode/aid.json (else " +
			"~/.config/opencode/aid.json), then the definition-of-done gate, whose \"ask\" the plugin turns into an " +
			"OpenCode permission request; an invalid config denies with what to fix. Exits non-zero only when aid " +
			"itself fails.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			stdin, _ := io.ReadAll(cmd.InOrStdin())
			v, err := guard.OpenCodeTool(stdin, guardEnv())
			if err != nil {
				_, _ = fmt.Fprintf(a.err, "aid-guards: opencode-tool: %v\n", err)
				return exitCode(1)
			}
			out, err := json.Marshal(v)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(a.out, string(out))
			return nil
		},
	}
}

// policyCmd prints the routing policy for a harness's orchestrator context.
func policyCmd(a *app) *cobra.Command {
	var harness string
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Print the routing policy for the orchestrator's context",
		Long: "Prints the routing policy: Claude Code's session-start text, or with --harness opencode the " +
			"OpenCode wording the plugin adds to the orchestrator's context. No skill body: the orchestrator " +
			"already carries aid-orchestrate.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			h, err := harnessOf(harness)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(a.out, guard.Policy(h))
			return nil
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "claude", "claude or opencode")
	return cmd
}
