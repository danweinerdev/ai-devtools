"""Shared helpers for the Claude guards: project config, name handling, fail-open runner.

Project config is `<cwd>/.claude/aid.json`:
    {"models": {"<agent>": "<model>"},
     "guards": {"inline_budget": 20, "exempt_verification": true},
     "sensitive": {"rules": [{"name": "...", "patterns": [...], "paths": [...],
                              "allow_agents": [...]}]}}
A missing file is an empty config (guards use defaults). An invalid one fails
loudly: `load` raises ConfigError, and `run` turns that into a deny that says
what to fix. Only a bug in a guard's own code fails open.
"""
import difflib
import json
import os
import re
import sys

AGENT_TOOLS = {"Agent", "Task"}
GENERIC_AGENTS = {"general-purpose", "explore", "plan", "claude"}
ORCHESTRATOR = "aid-orchestrator"
DEFAULT_BUDGET = 20
CONFIG_PATH = os.path.join(".claude", "aid.json")
MODELS = ("haiku", "sonnet", "opus", "fable")
TOP_KEYS = ("models", "guards", "sensitive")
GUARD_KEYS = ("inline_budget", "exempt_verification")
SENSITIVE_KEYS = ("rules",)
RULE_KEYS = ("name", "patterns", "paths", "allow_agents")


class ConfigError(Exception):
    """`.claude/aid.json` exists but is invalid; carries every problem found."""

    def __init__(self, path, problems):
        self.path = path
        self.problems = problems
        super().__init__(self.message())

    def message(self):
        lines = "\n".join(f"  - {p}" for p in self.problems)
        return (f"{self.path} is invalid, so the aid guards refuse to run on a "
                f"partial config. Fix:\n{lines}\n"
                "AID_GUARDS=off disables the guards for the session.")


def bare(name):
    """`ai-development:aid-explorer` -> `aid-explorer`; non-strings -> ''."""
    if not isinstance(name, str):
        return ""
    return name.strip().rsplit(":", 1)[-1].strip()


def guards_off():
    return os.environ.get("AID_GUARDS", "").strip().lower() == "off"


def _unknown(keys, allowed, where, problems):
    for key in keys:
        if key not in allowed:
            close = difflib.get_close_matches(str(key), allowed, n=1)
            hint = f" (did you mean `{close[0]}`?)" if close else ""
            problems.append(f"{where}: unknown key `{key}`{hint}; "
                            f"allowed: {', '.join(allowed)}")


def _string_list(value, where, problems):
    if not isinstance(value, list) or not all(isinstance(v, str) and v for v in value):
        problems.append(f"{where}: must be a list of non-empty strings")
        return []
    return value


def validate(data):
    """Every problem in a parsed config, as human-readable lines; [] when valid."""
    if not isinstance(data, dict):
        return ["the top level must be a JSON object"]
    problems = []
    _unknown(data, TOP_KEYS, "top level", problems)

    models = data.get("models", {})
    if not isinstance(models, dict):
        problems.append("models: must be an object of agent name -> model")
    else:
        for agent, model in models.items():
            if not agent.strip():
                problems.append("models: agent names must be non-empty")
            elif bare(agent).lower() in GENERIC_AGENTS or bare(agent) == ORCHESTRATOR:
                problems.append(f"models.{agent}: generic agents and the orchestrator "
                                "are never spawned, so an override has no effect")
            if model not in MODELS:
                problems.append(f"models.{agent}: `{model}` is not one of "
                                f"{', '.join(MODELS)}")

    guards = data.get("guards", {})
    if not isinstance(guards, dict):
        problems.append("guards: must be an object")
    else:
        _unknown(guards, GUARD_KEYS, "guards", problems)
        budget = guards.get("inline_budget", DEFAULT_BUDGET)
        if isinstance(budget, bool) or not isinstance(budget, int) or budget < 0:
            problems.append("guards.inline_budget: must be a non-negative integer")
        if not isinstance(guards.get("exempt_verification", True), bool):
            problems.append("guards.exempt_verification: must be true or false")

    if "sensitive" in data:
        sensitive = data["sensitive"]
        if not isinstance(sensitive, dict) or not isinstance(sensitive.get("rules"), list):
            problems.append('sensitive: must be {"rules": [...]}')
        else:
            _unknown(sensitive, SENSITIVE_KEYS, "sensitive", problems)
            for i, rule in enumerate(sensitive["rules"]):
                where = f"sensitive.rules[{i}]"
                if not isinstance(rule, dict):
                    problems.append(f"{where}: must be an object")
                    continue
                _unknown(rule, RULE_KEYS, where, problems)
                if not isinstance(rule.get("name"), str) or not rule.get("name"):
                    problems.append(f"{where}.name: required, a non-empty string")
                else:
                    where = f"sensitive.rules[{i}] ({rule['name']})"
                patterns = _string_list(rule.get("patterns", []), f"{where}.patterns", problems)
                paths = _string_list(rule.get("paths", []), f"{where}.paths", problems)
                _string_list(rule.get("allow_agents", []), f"{where}.allow_agents", problems)
                if not patterns and not paths:
                    problems.append(f"{where}: needs at least one pattern or path")
                for pat in patterns:
                    try:
                        re.compile(pat)
                    except re.error as exc:
                        problems.append(f"{where}.patterns: `{pat}` is not a valid "
                                        f"regular expression ({exc})")
    return problems


def load(cwd=None):
    """The validated project config; {} when absent; ConfigError when invalid."""
    path = os.path.join(cwd or os.getcwd(), CONFIG_PATH)
    try:
        with open(path) as f:
            text = f.read()
    except FileNotFoundError:
        return {}
    except OSError as exc:
        raise ConfigError(CONFIG_PATH, [f"cannot be read ({exc.strerror})"])
    try:
        data = json.loads(text)
    except ValueError as exc:
        raise ConfigError(CONFIG_PATH, [f"is not valid JSON ({exc})"])
    problems = validate(data)
    if problems:
        raise ConfigError(CONFIG_PATH, problems)
    return data


def section(config, key):
    return config.get(key, {})


def inline_budget(config):
    return section(config, "guards").get("inline_budget", DEFAULT_BUDGET)


def read_payload():
    """The hook payload as a dict, or None when stdin is unusable."""
    try:
        payload = json.load(sys.stdin)
    except (OSError, ValueError):
        return None
    return payload if isinstance(payload, dict) else None


def tool_input_of(payload):
    value = payload.get("tool_input")
    return value if isinstance(value, dict) else {}


def run(main):
    """Run a guard: AID_GUARDS=off allows; an invalid config denies with what to
    fix; any other exception is a guard bug and allows with a stderr note."""
    if guards_off():
        return 0
    try:
        return main() or 0
    except ConfigError as exc:
        print(f"aid-guards: {exc.message()}", file=sys.stderr)
        return 2
    except Exception as exc:  # noqa: BLE001 - a guard's own bug fails open, never wedging a session
        print(f"aid-guards: {os.path.basename(sys.argv[0])} errored "
              f"({exc.__class__.__name__}: {exc}); allowing (fail open).",
              file=sys.stderr)
        return 0
