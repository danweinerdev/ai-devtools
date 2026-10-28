#!/usr/bin/env python3
"""PreToolUse (Agent|Task): deny generic delegation and the orchestrator.

Generic agents run at the orchestrator's model, so spawning them is delegation
at orchestration cost. Roster (`aid-*`, bare or namespaced) and project agents
are never gated. Exit 2 with the reason on stderr denies.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import router_config as rc  # noqa: E402

ROLES = ("aid-mechanic for well-specified building, aid-oracle for high-risk "
         "work or deep review, aid-explorer for research, aid-designer for "
         "visual/UX, aid-creator for first-pass creative work, aid-craftsman "
         "for refining a working first pass")


def reason(name):
    key = name.lower()
    if name == rc.ORCHESTRATOR:
        return ("aid-orchestrator is the main conversation, never a subagent. "
                "Do its work in the main loop.")
    if key == "explore":
        return "Explore is blocked. Use aid-explorer for read-only research."
    if key == "plan":
        return ("Plan is blocked. Stay in the main loop and use the aid-think "
                "and aid-make-breakdown skills.")
    if key == "claude":
        return f"claude is blocked (generic). Pick the matching aid-* role: {ROLES}."
    if key == "general-purpose":
        return f"general-purpose is blocked. Pick the matching aid-* role: {ROLES}."
    return (f"Agent calls need a subagent_type. Pick the matching aid-* role: "
            f"{ROLES}.")


def main():
    payload = rc.read_payload()
    if payload is None or payload.get("tool_name") not in rc.AGENT_TOOLS:
        return 0
    name = rc.bare(rc.tool_input_of(payload).get("subagent_type"))
    if name == rc.ORCHESTRATOR or not name or name.lower() in rc.GENERIC_AGENTS:
        print("aid-guards: " + reason(name), file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(rc.run(main))
