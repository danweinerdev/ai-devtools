package guard

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// policy is the routing policy printed at session start, after the
// aid-orchestrate skill body.
const policy = `## Routing policy (aid)

Roster roles (spawn these by name; plugin agents may appear as ` + "`ai-development:aid-<role>`" + `):
- aid-explorer: read-only research. Evidence-backed findings, conflicts, search trails.
- aid-mechanic: well-specified technical building. Stops on mismatch.
- aid-oracle: high-risk or novel implementation, and independent single-lens review. A deliberate role, not an escalation fallback; spawn it directly.
- aid-designer: visual and UX direction and design review. No production code.
- aid-creator: first-pass creative work, marked with its soft spots.
- aid-craftsman: refine a working first pass into a finished one.

Stay in the main loop for: the user conversation, judgement, the PRD gate, synthesis, planning, and one-shot deterministic commands whose output is the answer. Delegate discovery that reads far more than it answers.

Built-ins are blocked and replaced:
- general-purpose -> the matching aid-* role (building: aid-mechanic; high-risk or deep review: aid-oracle; research: aid-explorer).
- Explore -> aid-explorer.
- Plan -> stay in the main loop with the aid-think and aid-make-breakdown skills.
- aid-orchestrator is the main conversation, never a subagent.

Project agents in .claude/agents/ are first-class routing targets; reviewer-described ones join Review as extra lenses. Discover them when needed.
An optional .claude/aid.json can override the model per agent; the guards apply it on spawn.
`

var frontmatter = regexp.MustCompile(`(?s)\A---\n.*?\n---\n`)

// sessionStart prints the aid-orchestrate skill body, then the routing
// policy, then any problems with .claude/aid.json. It never reads docs/PRDs or
// any PRD index, and it always exits 0: session start never blocks a session,
// so even AID_GUARDS=off keeps the policy.
func sessionStart(p *Object, env Env) (d Decision) {
	defer func() {
		if r := recover(); r != nil {
			d = Decision{Stdout: d.Stdout, Stderr: "aid-guards: session-start errored; continuing.\n"}
		}
	}()
	var out strings.Builder
	if body := skillBody(env.Getenv("CLAUDE_PLUGIN_ROOT")); body != "" {
		out.WriteString(body + "\n\n")
	}
	out.WriteString(policy + "\n")
	if _, err := Load(cwdOf(p, env)); err != nil {
		var ce *ConfigError
		if errors.As(err, &ce) {
			out.WriteString("## Configuration error (aid)\n\n" + ce.Error() + "\n\n" +
				"Until it is fixed, every guard that reads it denies its call. Tell the user now.\n")
		}
	}
	return Decision{Stdout: out.String()}
}

// skillBody is the aid-orchestrate skill without its frontmatter, or "" when
// the plugin root or the file is missing.
func skillBody(root string) string {
	if root == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(root, "skills", "aid-orchestrate", "SKILL.md"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(frontmatter.ReplaceAllString(string(raw), ""))
}
