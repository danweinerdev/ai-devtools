#!/usr/bin/env python3
"""PreToolUse (Agent|Task) sensitivity gate. Opt-in: off unless configured.

Config: `.claude/aid.json` key `sensitive`, {"rules": [...]}, each rule like:
    {"name": "pii", "patterns": ["\\bNL\\d{2}[A-Z0-9]{10,}\\b"],
     "paths": ["secrets/**", "*.pem"], "allow_agents": []}
A rule matches when a regex in `patterns` matches the spawn text or a glob in
`paths` matches a token in it. On a match the spawn is denied unless the
target agent (bare name) is in `allow_agents`.

An invalid config (validated in router_config) denies with what to fix, as for
every guard; any other exception is a guard bug and fails open.
"""
import fnmatch
import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import router_config as rc  # noqa: E402


def collect_strings(value, out):
    if isinstance(value, str):
        out.append(value)
    elif isinstance(value, dict):
        for v in value.values():
            collect_strings(v, out)
    elif isinstance(value, list):
        for v in value:
            collect_strings(v, out)


def block(msg):
    print(f"aid-guards: {msg}", file=sys.stderr)
    return 2


def main():
    payload = rc.read_payload()
    if payload is None or payload.get("tool_name") not in rc.AGENT_TOOLS:
        return 0
    config = rc.load(payload.get("cwd") or os.getcwd())
    if "sensitive" not in config:
        return 0  # opt-in
    rules = config["sensitive"]["rules"]

    tool_input = rc.tool_input_of(payload)
    target = rc.bare(tool_input.get("subagent_type"))
    strings = []
    collect_strings(tool_input, strings)
    text = "\n".join(strings)
    tokens = text.split()

    for rule in rules:
        name = rule["name"]
        allow = {rc.bare(a) for a in rule.get("allow_agents", [])}
        hit = any(re.search(pat, text) for pat in rule.get("patterns", []))
        if not hit:
            hit = any(fnmatch.fnmatch(tok, g)
                      for g in rule.get("paths", []) for tok in tokens)
        if hit and target not in allow:
            return block(f"sensitivity rule '{name}' matched content in this "
                         f"spawn and '{target or 'no agent'}' is not in its "
                         "allow_agents. Handle this data inline in the main "
                         "loop. (Rules live in .claude/aid.json.)")
    return 0


if __name__ == "__main__":
    sys.exit(rc.run(main))
