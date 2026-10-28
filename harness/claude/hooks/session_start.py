#!/usr/bin/env python3
"""SessionStart: print the aid-orchestrate skill body, then the routing policy,
then any problems with `.claude/aid.json`.

Never reads docs/PRDs or any PRD index. A missing skill file is skipped.
"""
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import router_config as rc  # noqa: E402

POLICY = """\
## Routing policy (aid)

Roster roles (spawn these by name; plugin agents may appear as `ai-development:aid-<role>`):
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
"""


def skill_body(root):
    try:
        text = open(os.path.join(root, "skills", "aid-orchestrate", "SKILL.md")).read()
    except OSError:
        return ""
    return re.sub(r"\A---\n.*?\n---\n", "", text, count=1, flags=re.S).strip()


def main():
    root = os.environ.get("CLAUDE_PLUGIN_ROOT") or os.path.dirname(
        os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
    body = skill_body(root)
    if body:
        print(body + "\n")
    print(POLICY)
    payload = rc.read_payload() or {}
    cwd = payload.get("cwd") if isinstance(payload.get("cwd"), str) else os.getcwd()
    try:
        rc.load(cwd)
    except rc.ConfigError as exc:
        print("## Configuration error (aid)\n\n" + exc.message() + "\n\n"
              "Until it is fixed, every guard that reads it denies its call. "
              "Tell the user now.")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"aid-guards: session_start errored ({exc.__class__.__name__}); "
              "continuing.", file=sys.stderr)
    sys.exit(0)
