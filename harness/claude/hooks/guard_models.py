#!/usr/bin/env python3
"""PreToolUse (Agent|Task): apply `.claude/aid.json` `models` overrides.

If the spawned agent's bare name is in `models`, rewrite the spawn's `model`
via hookSpecificOutput.updatedInput (which replaces the whole input, so the
original fields are copied). Claude Code requires a permissionDecision beside
updatedInput; "allow" is used. Generic agents and the orchestrator are never
touched (guard_generic denies them whatever this file says).
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import router_config as rc


def main():
    payload = rc.read_payload()
    if payload is None or payload.get("tool_name") not in rc.AGENT_TOOLS:
        return 0
    tool_input = rc.tool_input_of(payload)
    name = rc.bare(tool_input.get("subagent_type"))
    if not name or name == rc.ORCHESTRATOR or name.lower() in rc.GENERIC_AGENTS:
        return 0
    models = rc.section(rc.load(payload.get("cwd") or os.getcwd()), "models")
    model = models.get(name)
    if not isinstance(model, str) or not model or tool_input.get("model") == model:
        return 0
    print(json.dumps({"hookSpecificOutput": {
        "hookEventName": "PreToolUse",
        "permissionDecision": "allow",
        "permissionDecisionReason": f"aid.json model override for {name}: {model}",
        "updatedInput": {**tool_input, "model": model},
    }}))
    return 0


if __name__ == "__main__":
    sys.exit(rc.run(main))
