package guard

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// OpenCode's tool guard. The plugin's tool hook hands aid each call as
//
//	{"tool": "shell", "args": {...}, "sessionID": "...", "agent": "aid-orchestrator", "cwd": "..."}
//
// and aid answers {"decision": "allow"|"deny", "reason": "..."}. The call is
// translated into the payload the Claude Code guards read and run through the
// same three, reading .opencode/aid.json: generic, sensitive, inline. The
// model override has no OpenCode analogue: the plugin sets each agent's model
// when it loads.

// openCodeTools maps OpenCode 2 tool names to the Claude Code tools whose
// guards apply. Every other tool passes.
var openCodeTools = map[string]string{
	"shell": "Bash", "read": "Read", "grep": "Grep", "glob": "Glob", "subagent": "Agent",
	"write": "Write", "edit": "Edit", "patch": "Patch",
}

// OpenCode 2.2.0's built-in agents, as `opencode debug agents` lists them.
var (
	openCodeGeneric   = []string{"general", "explore"}             // its generic subagents
	openCodePrimaries = []string{"build", "plan"}                  // primary agents, never subagents
	openCodeInternal  = []string{"compaction", "summary", "title"} // hidden, internal
)

// roster is the aid-* specialists, which run as subagents.
var roster = []string{"aid-explorer", "aid-mechanic", "aid-oracle", "aid-designer", "aid-creator", "aid-craftsman"}

// openCodeGuards run in order; the first deny decides, and an ask stands
// only when none denies.
var openCodeGuards = []func(*Object, Env) (Decision, error){genericOpenCode, sensitive, inline, definitionOfDone}

// Verdict is the opencode-tool guard's answer. For "ask" the plugin raises an
// OpenCode permission request about Task and the call waits for the user.
type Verdict struct {
	Decision string `json:"decision"` // "allow", "ask" or "deny"
	Reason   string `json:"reason"`
	Task     string `json:"task,omitempty"`
}

// OpenCodeTool decides one OpenCode tool call. Both decisions are a Verdict;
// an error means aid itself failed (stdin unusable, a guard bug), and the
// plugin then fails open.
func OpenCodeTool(stdin []byte, env Env) (v Verdict, err error) {
	allow := Verdict{Decision: "allow"}
	if strings.ToLower(strings.TrimSpace(env.Getenv("AID_GUARDS"))) == "off" {
		return allow, nil
	}
	in := payloadOf(stdin)
	if in == nil {
		return Verdict{}, errors.New("stdin is not a JSON object")
	}
	tool, ok := openCodeTools[str(in.Get("tool"))]
	if !ok {
		return allow, nil
	}
	defer func() {
		if r := recover(); r != nil {
			v, err = Verdict{}, fmt.Errorf("guard errored (%v)", r)
		}
	}()
	env.Harness = OpenCode
	p := claudePayload(in, tool)
	verdict := allow
	for _, g := range openCodeGuards {
		d, gerr := g(p, env)
		var ce *ConfigError
		if errors.As(gerr, &ce) {
			return Verdict{Decision: "deny", Reason: ce.Error()}, nil
		}
		if gerr != nil {
			return Verdict{}, gerr
		}
		if d.Exit == 2 {
			return Verdict{Decision: "deny", Reason: strings.TrimSpace(strings.TrimPrefix(d.Stderr, "aid-guards: "))}, nil
		}
		if d.Ask != "" {
			verdict = Verdict{Decision: "ask", Reason: d.Ask, Task: d.Task}
		}
	}
	return verdict, nil
}

// claudePayload is the OpenCode call in the shape the Claude Code guards read:
// every argument carried over, plus the Claude names the guards look for.
func claudePayload(in *Object, tool string) *Object {
	args, _ := in.Get("args").(*Object)
	ti := &Object{Values: map[string]any{}}
	if args != nil {
		for _, k := range args.Keys {
			ti.Set(k, args.Values[k])
		}
	}
	switch tool {
	case "Grep":
		if args.Has("include") {
			ti.Set("glob", args.Get("include")) // Grep's path filter
		}
	case "Agent":
		ti.Set("subagent_type", args.Get("agent"))
		// A subagent call blocks the caller unless it says otherwise, so it's
		// foreground and resets the inline budget.
		ti.Set("run_in_background", truthy(args.Get("background")))
	}
	p := &Object{Values: map[string]any{}}
	p.Set("tool_name", tool)
	p.Set("tool_input", ti)
	p.Set("cwd", in.Get("cwd"))
	p.Set("session_id", in.Get("sessionID"))
	// The plugin bumps turnID on each user prompt, so the inline budget is
	// per turn as on Claude; without it the budget spans the session.
	p.Set("prompt_id", in.Get("turnID"))
	if openCodeSubagent(in) {
		p.Set("agent_type", str(in.Get("agent"))) // a worker's calls are never throttled
	}
	return p
}

// openCodeSubagent: the calling agent is a subagent. The optional agent_mode
// field decides when the plugin sends it; otherwise the roster and OpenCode's
// generic subagents are subagents, and anything else counts as a primary,
// which only makes the inline budget stricter.
func openCodeSubagent(in *Object) bool {
	switch str(in.Get("agent_mode")) {
	case "subagent":
		return true
	case "primary":
		return false
	}
	agent := Bare(in.Get("agent"))
	return slices.Contains(roster, agent) || slices.Contains(openCodeGeneric, strings.ToLower(agent))
}

// genericOpenCode denies delegating to OpenCode's generic or non-subagent
// built-ins and to the orchestrator, pointing at the matching aid-* role.
func genericOpenCode(p *Object, _ Env) (Decision, error) {
	if str(p.Get("tool_name")) != "Agent" {
		return Decision{}, nil
	}
	name := Bare(toolInput(p).Get("subagent_type"))
	switch key := strings.ToLower(name); {
	case name == "":
		return deny("subagent calls need an agent. Pick the matching aid-* role: " + roles + "."), nil
	case name == orchestrator:
		return deny("aid-orchestrator is the main conversation, never a subagent. Do its work in the main loop."), nil
	case key == "explore":
		return deny("explore is blocked. Use aid-explorer for read-only research."), nil
	case key == "general":
		return deny("general is blocked (generic). Pick the matching aid-* role: " + roles + "."), nil
	case key == "plan":
		return deny("plan is a primary agent, not a subagent. Stay in the main loop and use the aid-think and " +
			"aid-make-breakdown skills."), nil
	case slices.Contains(openCodePrimaries, key):
		return deny(name + " is a primary agent, not a subagent. Pick the matching aid-* role: " + roles + "."), nil
	case slices.Contains(openCodeInternal, key):
		return deny(name + " is OpenCode's internal agent, not a delegation target. Pick the matching aid-* role: " +
			roles + "."), nil
	}
	return Decision{}, nil
}
