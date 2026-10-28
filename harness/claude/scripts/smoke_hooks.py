#!/usr/bin/env python3
"""Smoke-test the wired hooks: every referenced file exists, unusable input
fails open, odd-but-valid input decides cleanly (exit 0 or 2, no traceback),
and the key decisions come out right. Pipes sample JSON through each hook."""
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))))
MANIFEST = os.path.join(ROOT, "harness", "claude", "hooks.json")
REF = re.compile(r"\$\{CLAUDE_PLUGIN_ROOT\}(/[\w./-]+)")
UNUSABLE = ["not json {{{", "", '{"tool_name": "Agent"', "null", "[1, 2]", '"s"']
ODD = [{}, {"tool_name": 42, "tool_input": "nope", "cwd": None},
       {"tool_name": "Agent", "tool_input": None},
       {"tool_name": "Agent", "tool_input": {"subagent_type": ["x"], "prompt": {"a": [1]}}}]
failures = []


def fail(msg):
    failures.append(msg)
    print("FAIL " + msg, file=sys.stderr)


def find_bash():
    """The bash Claude Code runs hooks with. On Windows that's Git Bash: a bare
    `bash` from Python resolves to System32's WSL launcher, which isn't it."""
    if os.environ.get("AID_SMOKE_BASH"):
        return os.environ["AID_SMOKE_BASH"]
    if sys.platform == "win32":
        for base in filter(None, [os.environ.get("PROGRAMFILES"), os.environ.get("PROGRAMW6432")]):
            git_bash = os.path.join(base, "Git", "bin", "bash.exe")
            if os.path.exists(git_bash):
                return git_bash
    return shutil.which("bash") or "bash"


BASH = find_bash()


def run(command, stdin, cwd, env=None):
    return subprocess.run([BASH, "-c", command], input=stdin, text=True, capture_output=True,
                          cwd=cwd, timeout=30, check=False,
                          env={**os.environ, "CLAUDE_PLUGIN_ROOT": ROOT, **(env or {})})


def main():
    with open(MANIFEST) as f:
        manifest = json.load(f)
    hooks = [(ev, b.get("matcher"), h["command"])
             for ev, blocks in manifest["hooks"].items() for b in blocks
             for h in b["hooks"] if h.get("type") == "command"]
    print(f"manifest: {len(hooks)} wired hooks")
    for _, _, cmd in hooks:
        for rel in REF.findall(cmd):
            if not os.path.isfile(os.path.join(ROOT, rel.lstrip("/"))):
                fail(f"missing file {rel}")
    with tempfile.TemporaryDirectory() as cwd:
        for ev, matcher, cmd in hooks:
            label = f"{ev}[{matcher or '*'}]"
            for raw in UNUSABLE:
                p = run(cmd, raw, cwd)
                if p.returncode != 0 or "Traceback" in p.stderr:
                    fail(f"{label} did not fail open on {raw!r}: {p.stderr[-200:]}")
            for obj in ODD:
                p = run(cmd, json.dumps(obj), cwd)
                if p.returncode not in (0, 2) or "Traceback" in p.stderr:
                    fail(f"{label} exit {p.returncode} on {obj!r}: {p.stderr[-200:]}")
        agent = [c for ev, m, c in hooks if m == "Agent|Task"]

        def spawn(t):
            return json.dumps({"tool_name": "Agent", "cwd": cwd, "tool_input": {"subagent_type": t, "prompt": "p"}})

        if not any(run(c, spawn("general-purpose"), cwd).returncode == 2 for c in agent):
            fail("general-purpose spawn was not denied")
        if any(run(c, spawn("ai-development:aid-oracle"), cwd).returncode != 0 for c in agent):
            fail("namespaced aid-oracle spawn was denied")
        if any(run(c, spawn("general-purpose"), cwd, {"AID_GUARDS": "off"}).returncode != 0 for c in agent):
            fail("AID_GUARDS=off did not allow")
        os.makedirs(os.path.join(cwd, ".claude"))
        with open(os.path.join(cwd, ".claude", "aid.json"), "w") as f:
            json.dump({"models": {"aid-explorer": "sonnet"}}, f)
        outs = [run(c, spawn("aid-explorer"), cwd).stdout for c in agent]
        got = [json.loads(o)["hookSpecificOutput"]["updatedInput"]["model"] for o in outs if o.strip()]
        if got != ["sonnet"]:
            fail(f"model override not applied: {got}")
        start = next(c for ev, _, c in hooks if ev == "SessionStart")
        out = run(start, "{}", cwd).stdout
        if "Routing policy" not in out or "aid-explorer" not in out:
            fail("SessionStart did not print the routing policy")
    if failures:
        print(f"smoke: {len(failures)} failure(s)", file=sys.stderr)
        return 1
    print("smoke: ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
