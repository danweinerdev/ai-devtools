import json
import os
import subprocess
import sys
import tempfile
import unittest
import uuid
from typing import ClassVar

HOOKS = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "hooks")
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(HOOKS)))


def run(hook, payload, env=None, raw=None):
    full = {k: v for k, v in os.environ.items() if k != "AID_GUARDS"}
    full.update(env or {})
    return subprocess.run(
        [sys.executable, os.path.join(HOOKS, hook)],
        input=raw if raw is not None else json.dumps(payload),
        text=True, capture_output=True, env=full, check=False)


def spawn(subagent_type=None, cwd=None, **extra):
    ti = dict(extra)
    if subagent_type is not None:
        ti["subagent_type"] = subagent_type
    return {"tool_name": "Agent", "tool_input": ti, "cwd": cwd or "/nonexistent"}


class Project(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.cwd = self._tmp.name
        self.addCleanup(self._tmp.cleanup)

    def config(self, data):
        os.makedirs(os.path.join(self.cwd, ".claude"), exist_ok=True)
        with open(os.path.join(self.cwd, ".claude", "aid.json"), "w") as f:
            f.write(data if isinstance(data, str) else json.dumps(data))


class GenericGuard(unittest.TestCase):
    def test_denied(self):
        for name, hint in [("general-purpose", "aid-mechanic"), ("Explore", "aid-explorer"),
                           ("Plan", "aid-make-breakdown"), ("claude", "aid-oracle"),
                           (None, "aid-explorer"), ("", "aid-explorer"),
                           ("aid-orchestrator", "main conversation"),
                           ("ai-development:aid-orchestrator", "main conversation"),
                           ("plugin:general-purpose", "aid-mechanic")]:
            with self.subTest(name=name):
                p = run("guard_generic.py", spawn(name))
                self.assertEqual(p.returncode, 2, p.stderr)
                self.assertIn(hint, p.stderr)

    def test_task_tool_name_also_guarded(self):
        pl = spawn("general-purpose")
        pl["tool_name"] = "Task"
        self.assertEqual(run("guard_generic.py", pl).returncode, 2)

    def test_roster_and_project_agents_allowed(self):
        for name in ["aid-oracle", "aid-explorer", "ai-development:aid-explorer",
                     "ai-development:aid-oracle", "my-project-agent"]:
            with self.subTest(name=name):
                self.assertEqual(run("guard_generic.py", spawn(name)).returncode, 0)

    def test_models_file_does_not_unblock_generic(self):
        with tempfile.TemporaryDirectory() as cwd:
            os.makedirs(os.path.join(cwd, ".claude"))
            with open(os.path.join(cwd, ".claude", "aid.json"), "w") as f:
                json.dump({"models": {"general-purpose": "haiku"}}, f)
            self.assertEqual(run("guard_generic.py", spawn("general-purpose", cwd)).returncode, 2)
            self.assertEqual(run("guard_models.py", spawn("general-purpose", cwd)).stdout, "")

    def test_escape_hatch_and_fail_open(self):
        self.assertEqual(run("guard_generic.py", spawn("general-purpose"),
                             env={"AID_GUARDS": "off"}).returncode, 0)
        for raw in ["not json", "", "null", "[1]"]:
            self.assertEqual(run("guard_generic.py", None, raw=raw).returncode, 0)

    def test_internal_exception_fails_open(self):
        code = (f"import sys; sys.path.insert(0, {HOOKS!r}); import router_config as rc; "
                "sys.exit(rc.run(lambda: 1/0))")
        p = subprocess.run([sys.executable, "-c", code], capture_output=True, text=True, check=False)
        self.assertEqual(p.returncode, 0)
        self.assertIn("fail open", p.stderr)


class ModelGuard(Project):
    def test_rewrites_model_with_allow_and_keeps_fields(self):
        self.config({"models": {"aid-explorer": "sonnet"}})
        p = run("guard_models.py", spawn("aid-explorer", self.cwd, prompt="hi", model="haiku"))
        self.assertEqual(p.returncode, 0)
        out = json.loads(p.stdout)["hookSpecificOutput"]
        self.assertEqual(out["hookEventName"], "PreToolUse")
        self.assertEqual(out["permissionDecision"], "allow")
        self.assertEqual(out["updatedInput"],
                         {"subagent_type": "aid-explorer", "prompt": "hi", "model": "sonnet"})

    def test_namespaced_name_matches_bare_key(self):
        self.config({"models": {"aid-explorer": "sonnet"}})
        p = run("guard_models.py", spawn("ai-development:aid-explorer", self.cwd, prompt="x"))
        out = json.loads(p.stdout)["hookSpecificOutput"]["updatedInput"]
        self.assertEqual(out["model"], "sonnet")
        self.assertEqual(out["subagent_type"], "ai-development:aid-explorer")

    def test_project_agent_override(self):
        self.config({"models": {"reviewer": "opus"}})
        out = json.loads(run("guard_models.py", spawn("reviewer", self.cwd)).stdout)
        self.assertEqual(out["hookSpecificOutput"]["updatedInput"]["model"], "opus")

    def test_no_change_when_unlisted_missing_config_or_already_set(self):
        self.assertEqual(run("guard_models.py", spawn("aid-explorer", self.cwd)).stdout, "")
        self.config({"models": {"aid-explorer": "sonnet"}})
        self.assertEqual(run("guard_models.py", spawn("aid-oracle", self.cwd)).stdout, "")
        self.assertEqual(run("guard_models.py",
                             spawn("aid-explorer", self.cwd, model="sonnet")).stdout, "")

    def test_bad_config_denies_and_off(self):
        self.config("{broken")
        p = run("guard_models.py", spawn("aid-explorer", self.cwd))
        self.assertEqual((p.returncode, p.stdout), (2, ""))
        self.assertIn("not valid JSON", p.stderr)
        self.config({"models": {"aid-explorer": "sonnet"}})
        p = run("guard_models.py", spawn("aid-explorer", self.cwd), env={"AID_GUARDS": "off"})
        self.assertEqual(p.stdout, "")


class Sensitive(Project):
    RULE: ClassVar[dict] = {"rules": [{"name": "pii", "patterns": [r"SECRET-\d+"], "paths": ["*.pem"],
                       "allow_agents": ["aid-oracle"]}]}

    def test_off_without_config(self):
        self.assertEqual(run("guard_sensitive.py",
                             spawn("aid-explorer", self.cwd, prompt="SECRET-1")).returncode, 0)

    def test_pattern_and_path_match_deny(self):
        self.config({"sensitive": self.RULE})
        for prompt in ["see SECRET-42", "read key.pem"]:
            p = run("guard_sensitive.py", spawn("aid-explorer", self.cwd, prompt=prompt))
            self.assertEqual(p.returncode, 2, prompt)
            self.assertIn("pii", p.stderr)

    def test_allow_agents_bare_and_namespaced(self):
        self.config({"sensitive": self.RULE})
        for name in ["aid-oracle", "ai-development:aid-oracle"]:
            self.assertEqual(run("guard_sensitive.py",
                                 spawn(name, self.cwd, prompt="SECRET-1")).returncode, 0)

    def test_no_match_allows_and_off_switch(self):
        self.config({"sensitive": self.RULE})
        self.assertEqual(run("guard_sensitive.py",
                             spawn("aid-explorer", self.cwd, prompt="clean")).returncode, 0)
        self.assertEqual(run("guard_sensitive.py", spawn("aid-explorer", self.cwd, prompt="SECRET-1"),
                             env={"AID_GUARDS": "off"}).returncode, 0)

    def test_malformed_rules_and_broken_file_deny(self):
        for bad in [{"sensitive": "nope"},
                    {"sensitive": [{"name": "x", "patterns": ["a"]}]},
                    {"sensitive": {"rules": [{"name": "x", "patterns": ["("]}]}},
                    {"sensitive": {"rules": [{"patterns": ["a"]}]}},
                    {"sensitive": {"rules": [{"name": "x"}]}},
                    "{broken"]:
            with self.subTest(bad=bad):
                self.config(bad)
                p = run("guard_sensitive.py", spawn("aid-oracle", self.cwd, prompt="x"))
                self.assertEqual(p.returncode, 2, p.stderr)
                self.assertIn("is invalid", p.stderr)


class Config(Project):
    def deny_message(self, data):
        self.config(data)
        p = run("guard_models.py", spawn("aid-explorer", self.cwd))
        self.assertEqual(p.returncode, 2, p.stderr)
        return p.stderr

    def test_unknown_keys_get_did_you_mean(self):
        self.assertIn("did you mean `models`", self.deny_message({"model": {}}))
        self.assertIn("did you mean `inline_budget`",
                      self.deny_message({"guards": {"inline_budge": 5}}))
        self.assertIn("did you mean `allow_agents`", self.deny_message(
            {"sensitive": {"rules": [{"name": "x", "paths": ["*.pem"], "allow_agent": []}]}}))

    def test_bad_values_are_named(self):
        for data, needle in [
                ({"models": {"aid-explorer": "gpt-5"}}, "models.aid-explorer"),
                ({"models": {"general-purpose": "opus"}}, "no effect"),
                ({"models": []}, "models: must be an object"),
                ({"guards": {"inline_budget": "40"}}, "guards.inline_budget"),
                ({"guards": {"inline_budget": True}}, "guards.inline_budget"),
                ({"guards": {"exempt_verification": "yes"}}, "guards.exempt_verification"),
                ([], "top level must be a JSON object")]:
            with self.subTest(data=data):
                self.assertIn(needle, self.deny_message(data))

    def test_every_problem_reported_at_once(self):
        msg = self.deny_message({"models": {"aid-explorer": "gpt"}, "guards": {"inline_budget": -1}})
        self.assertIn("models.aid-explorer", msg)
        self.assertIn("guards.inline_budget", msg)

    def test_inline_guard_denies_but_config_file_stays_readable(self):
        self.config("{broken")
        pl = {"session_id": uuid.uuid4().hex, "prompt_id": "p", "cwd": self.cwd,
              "tool_name": "Grep", "tool_input": {"pattern": "x"}}
        self.assertEqual(run("guard_inline.py", pl).returncode, 2)
        pl["tool_name"], pl["tool_input"] = "Read", {"file_path": ".claude/aid.json"}
        self.assertEqual(run("guard_inline.py", pl).returncode, 0)

    def test_session_start_reports_errors(self):
        self.config({"models": {"aid-explorer": "gpt-5"}})
        p = run("session_start.py", {"cwd": self.cwd})
        self.assertEqual(p.returncode, 0)
        self.assertIn("Configuration error", p.stdout)
        self.assertIn("models.aid-explorer", p.stdout)
        self.config({"models": {"aid-explorer": "sonnet"}})
        self.assertNotIn("Configuration error", run("session_start.py", {"cwd": self.cwd}).stdout)


class Inline(Project):
    def setUp(self):
        super().setUp()
        self.session = uuid.uuid4().hex
        self.config({"guards": {"inline_budget": 3}})

    def call(self, tool="Grep", env=None, **ti):
        pl = {"session_id": self.session, "prompt_id": "p", "cwd": self.cwd,
              "tool_name": tool, "tool_input": ti}
        return run("guard_inline.py", pl, env=env)

    def exhaust(self, n=3):
        for _ in range(n):
            self.assertEqual(self.call(pattern="x").returncode, 0)

    def test_blocks_after_budget_pointing_at_explorer(self):
        self.exhaust()
        p = self.call(pattern="x")
        self.assertEqual(p.returncode, 2)
        self.assertIn("aid-explorer", p.stderr)

    def test_foreground_agent_resets_background_does_not(self):
        self.exhaust()
        self.call(pattern="x")
        self.call("Agent", subagent_type="aid-explorer", run_in_background=True)
        self.assertEqual(self.call(pattern="x").returncode, 2)
        self.call("Agent", subagent_type="aid-explorer", run_in_background=False)
        self.assertEqual(self.call(pattern="x").returncode, 0)

    def test_subagent_calls_never_counted(self):
        pl = {"session_id": self.session, "prompt_id": "p", "cwd": self.cwd, "agent_id": "a1",
              "tool_name": "Grep", "tool_input": {}}
        for _ in range(6):
            self.assertEqual(run("guard_inline.py", pl).returncode, 0)

    def test_prd_and_aid_dir_reads_exempt(self):
        self.exhaust()
        prd = os.path.join(self.cwd, "docs", "PRDs", "X.md")
        self.assertEqual(self.call("Read", file_path=prd).returncode, 0)
        self.assertEqual(self.call("Read", file_path=os.path.join(self.cwd, ".aid", "tasks.json")).returncode, 0)
        self.assertEqual(self.call("Glob", pattern="docs/PRDs/**/*.md").returncode, 0)
        self.assertEqual(self.call("Read", file_path=os.path.join(self.cwd, "src", "a.py")).returncode, 2)

    def test_mixed_paths_still_count(self):
        self.exhaust()
        os.makedirs(os.path.join(self.cwd, "src"))
        p = self.call("Bash", command="grep -r foo docs/PRDs src")
        self.assertEqual(p.returncode, 2)

    def test_aid_cli_exempt(self):
        self.exhaust()
        self.assertEqual(self.call("Bash", command="aid graph list | head").returncode, 0)
        self.assertEqual(self.call("Bash", command="aid graph list; grep -r foo src").returncode, 2)

    def test_verification_exempt(self):
        self.exhaust()
        for cmd in ["git log --oneline", "pytest -q", "ps aux | wc -l", "make check"]:
            self.assertEqual(self.call("Bash", command=cmd).returncode, 0, cmd)
        self.assertEqual(self.call("Bash", command="grep -r foo src").returncode, 2)

    def test_off_and_fail_open(self):
        self.exhaust()
        self.assertEqual(self.call(pattern="x", env={"AID_GUARDS": "off"}).returncode, 0)
        self.assertEqual(run("guard_inline.py", None, raw="garbage").returncode, 0)

    def test_default_budget_without_config(self):
        os.remove(os.path.join(self.cwd, ".claude", "aid.json"))
        for _ in range(20):
            self.assertEqual(self.call(pattern="x").returncode, 0)
        self.assertEqual(self.call(pattern="x").returncode, 2)


class SessionStart(unittest.TestCase):
    def test_prints_policy_without_skill_and_with_skill(self):
        with tempfile.TemporaryDirectory() as plugin:
            env = {"CLAUDE_PLUGIN_ROOT": plugin}
            p = run("session_start.py", {}, env=env)
            self.assertEqual(p.returncode, 0)
            self.assertIn("aid-explorer", p.stdout)
            self.assertIn("project agents", p.stdout.lower())
            os.makedirs(os.path.join(plugin, "skills", "aid-orchestrate"))
            with open(os.path.join(plugin, "skills", "aid-orchestrate", "SKILL.md"), "w") as f:
                f.write("---\nname: aid-orchestrate\n---\nORCH-BODY\n")
            p = run("session_start.py", {}, env=env)
            self.assertTrue(p.stdout.startswith("ORCH-BODY"))
            self.assertNotIn("name: aid-orchestrate", p.stdout)
            self.assertLess(p.stdout.index("ORCH-BODY"), p.stdout.index("Routing policy"))

    def test_never_mentions_prd_index(self):
        p = run("session_start.py", {})
        self.assertNotIn("PRD index", p.stdout)


if __name__ == "__main__":
    unittest.main()
