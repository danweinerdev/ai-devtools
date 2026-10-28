#!/usr/bin/env python3
"""PreToolUse inline-exploration budget.

After `guards.inline_budget` (default 20) search-type tool calls in one user
turn, further ones are denied with a pointer to aid-explorer. A foreground
Agent call resets the budget; a background one does not. Subagent tool calls
are never counted.

Not counted:
- verification/inspection commands (tests, linters, git log/show, process
  inspection, counting pipes; see `is_verification_op`),
- operations whose every path is under `docs/PRDs/` or `.aid/`,
- `aid` CLI invocations.

Config: `.claude/aid.json` `{"guards": {"inline_budget": 20, "exempt_verification": true}}`.
"""
import json
import os
import re
import sys
import tempfile

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import router_config as rc  # noqa: E402

SEARCHY_TOOLS = {"Read", "Grep", "Glob"}
# a searchy word counts at any command position (start, or after ; && || | ` $( or
# newline, with optional VAR=x assignments), so prefixes like `cd x && grep` or
# `export F=1; rg` cannot dodge the counter. Words in ordinary arguments do not match.
SEARCHY_BASH = re.compile(
    r"(?:^|[;&|`\n]|\$\()\s*(?:\w+=\S*\s+)*"
    r"(?:git\s+(?:-\w+\s+|--\S+\s+)*)?"  # `git grep` is a code search, not history
    r"(rg|grep|find|fd|ls|tree|cat|head|tail|awk|jq|yq)\b")
# stdout redirected to a file means the command writes, it does not explore.
# `2>` / `2>&1` are stderr plumbing common in real searches; keep counting those.
WRITE_REDIRECT = re.compile(r"(?<![\d&])>|&>")
# Path-shaped argument tokens: anything containing a separator, plus bare
# capitalised words that could name an artifact directory (Plans, Specs).
# Flags are dropped by the caller. A token wrongly judged "not a path" only
# makes the guard stricter, never laxer.
PATH_TOKEN = re.compile(r"^[~./]?[\w.@+-]+(?:/[\w.*?@+-]*)+/?$|^[A-Z][\w-]*/?$")

# --- verification / inspection ------------------------------------------
# The budget polices *exploration*: work where answering the question means
# ingesting substantially more text than the answer contains. Verification is
# the opposite shape -- the command is already known and its output IS the
# answer -- so counting it measures the wrong thing. `grep -c` never explores;
# `grep -r foo src/` usually does.
#
# Machine state: a process check, a disk sample, a memory reading. Polling one
# of these every 60s is a single measurement sampled over time, not N
# explorations.
INSPECT_CMD = re.compile(
    r"(?:^|[;&|`\n]|\$\()\s*(?:\w+=\S*\s+)*"
    r"(ps|top|htop|du|df|free|uptime|uname|whoami|id|date|lsof|ss|netstat|"
    r"nproc|stat|file|wc|which|command|type|env|printenv|sha\d*sum|md5sum|"
    r"cksum|b2sum)\b")
# Reading /proc or /sys is a machine-state probe whatever tool does it.
PROC_READ = re.compile(r"(?:^|[\s'\"=])/(?:proc|sys)/")
# Repository history. `git show <sha>`/`git log` verify a claim about what
# changed; they do not sweep the tree. `git grep` is deliberately absent -- it
# is a code search wearing a git prefix.
GIT_VERIFY = re.compile(
    r"(?:^|[;&|`\n]|\$\()\s*(?:\w+=\S*\s+)*git\s+(?:-\w+\s+|--\S+\s+)*"
    r"(show|log|diff|rev-parse|status|blame|describe|tag|branch|remote|"
    r"cat-file|shortlog|reflog|check-ignore|ls-remote)\b")
# Test runners, linters, type checkers, validators. Running the suite to
# confirm a fix is the cheapest possible verification; throttling it pushes the
# loop toward *assuming* the fix worked, which is strictly worse.
#
# Dedicated tools match on their own name -- `ruff`, `pytest`, `shellcheck` do
# one thing and that thing is verification. Interpreters and multi-purpose
# build tools do NOT: `python3 scripts/dump_everything.py`, `npx tsx
# explore.ts` and `go run ./cmd/scan` are arbitrary programs, and exempting the
# bare binary would let any script launder unlimited exploration through it.
# Those must name a test/lint subcommand to qualify. `jq`/`yq` are absent
# entirely -- they are in SEARCHY_BASH precisely because reading a document
# through them is exploration.
VERIFY_CMD = re.compile(
    r"(?:^|[;&|`\n]|\$\()\s*(?:\w+=\S*\s+)*(?:"
    # Single-purpose verification tools: the name alone is the tell.
    r"(?:pytest|py\.test|tox|nox|nosetests|jest|vitest|mocha|ava|"
    r"jasmine|karma|phpunit|rspec|minitest|ctest|gtest|"
    r"ruff|flake8|pylint|black|isort|mypy|pyright|pyre|bandit|vulture|"
    r"eslint|prettier|tsc|biome|stylelint|"
    r"rubocop|golangci-lint|gofmt|govet|staticcheck|clippy|rustfmt|"
    r"clang-tidy|clang-format|cppcheck|shellcheck|shfmt|hadolint|"
    r"tflint|kubeconform|kubeval|ansible-lint|yamllint|xmllint)\b"
    # Interpreters/build tools: only in a test- or lint-shaped invocation.
    r"|python3?\s+-m\s+(?:pytest|unittest|tox|nox|mypy|ruff|flake8|pylint|"
    r"black|isort|pyright|bandit|compileall)\b"
    r"|(?:npm|pnpm|yarn|bun)\s+(?:run\s+)?(?:test|lint|typecheck|check)\b"
    r"|(?:go|cargo|dotnet|swift|mvn|gradle)\s+(?:test|vet|fmt|check|clippy)\b"
    r"|(?:make|ninja|bazel|just|task)\s+(?:test|check|lint|verify)\b"
    r"|terraform\s+(?:validate|fmt)\b"
    r"|helm\s+(?:lint|template)\b"
    r")")
# `... | wc -l` (or -c/-w) is counting, not reading: the answer is one number
# however many bytes were scanned. This is the single clearest instance of the
# "reads no more than it answers" rule.
COUNTING_PIPE = re.compile(r"\|\s*(?:\w+=\S*\s+)*wc\b(?:\s+-[lcwm]+)?\s*$")
# `grep -c` counts matches; `grep -l`/`-L` list filenames only. Both return a
# summary whose size is independent of what was scanned.
GREP_COUNTING = re.compile(
    r"(?:^|[;&|`\n]|\$\()\s*(?:rg|grep|egrep|fgrep)\b"
    r"(?=(?:\s+-\S+)*\s+-\w*[clL])")
# A listing long enough to need paging is a listing too long to be the answer.
# `du -sh /tmp` inspects; `du -sh /tmp/* | head -50` browses. Applies to every
# verification form, not just INSPECT_CMD: `git log -p -- src/ | head -500` is
# a code sweep wearing history's clothes.
#
# `sort`/`awk`/`cut` are absent deliberately: they reshape a fixed-size table
# (`df -h | sort -k5`) without enlarging what is read, so they leave the "reads
# no more than it answers" property intact.
# The `(?!...)` spares a counting filter: `ps -ef | grep -c python` returns one
# number, so the grep is not paging a listing, it is summarising one.
PAGED = re.compile(
    r"\|\s*(?:\w+=\S*\s+)*(?:head|tail|less|more|"
    r"(?:grep|egrep|fgrep|rg)\b(?!(?:\s+-\S+)*\s+-\w*[clL]\b))\b")
# Command separators: a compound command is verification only if *every* clause
# is. `date; grep -r secret .` must not launder the sweep behind the `date`.
CLAUSE_SPLIT = re.compile(r"(?:\|\||&&|[;&\n])")

PATH_FIELDS = ("file_path", "path", "notebook_path", "filePath")
# Glob and Grep often carry the directory inside the pattern rather than in a
# path field (`Glob: ".plans/**/*.md"`), so the pattern's literal prefix has to
# count as a path or artifact globbing looks unscoped and gets throttled.
GLOB_META = set("*?[]{}!")

EXEMPT_DIRS = (("docs", "PRDs"), (".aid",), (".claude", "aid.json"))
# `aid ...` at a command position, e.g. `aid graph list`.
AID_CLI = re.compile(r"^\s*(?:\w+=\S*\s+)*aid(?:\s|$)")


def counter_path(payload):
    key = f"{payload.get('session_id', 'unknown')}-{payload.get('prompt_id', 'unknown')}"
    return os.path.join(tempfile.gettempdir(), f"aid-inline-{key}")


def bash_path_tokens(command, cwd):
    """Argument tokens from a shell command that name paths."""
    tokens = []
    for raw in re.split(r"[\s;|&]+", command):
        token = raw.strip("'\"`()")
        if not token or token.startswith("-"):
            continue
        if "=" in token and not token.startswith("/"):
            continue
        if PATH_TOKEN.match(token):
            tokens.append(token)
            continue
        # A bare word naming something on disk is a path argument
        # (`grep foo src`), not a search pattern -- and bare lowercase
        # directory names are exactly how code trees get swept in alongside
        # artifact directories. Mistaking a pattern for a path only makes the
        # guard stricter, never laxer, so resolve the ambiguity that way.
        try:
            if os.path.exists(os.path.join(cwd, os.path.expanduser(token))):
                tokens.append(token)
        except (OSError, ValueError):
            continue
    return tokens


def glob_literal_prefix(pattern):
    """The leading path components of a glob that contain no wildcards.

    `.plans/**/*.md` -> `.plans`, `/abs/Specs/*.md` -> `/abs/Specs`,
    `**/*.py` -> None. Only the literal head is a path; the wildcard tail could
    match anywhere, so a pattern with no literal head stays unscoped and is
    counted like any other broad search.
    """
    if not isinstance(pattern, str) or not pattern:
        return None
    parts = []
    for part in pattern.split("/"):
        if not part:
            if not parts:
                parts.append("")  # leading empty: absolute pattern
                continue
            break
        if set(part) & GLOB_META:
            break
        parts.append(part)
    if not parts or parts == [""]:
        return None
    return "/".join(parts) or None


def candidate_paths(tool, tool_input, cwd):
    if tool == "Bash":
        return bash_path_tokens(tool_input.get("command", "") or "", cwd)
    paths = []
    for field in PATH_FIELDS:
        value = tool_input.get(field)
        if isinstance(value, str) and value:
            paths.append(value)
    # Glob's `pattern` is a path glob. Grep's `pattern` is a regex to search
    # for, never a path -- only its `glob` field filters paths.
    pattern_field = {"Glob": "pattern", "Grep": "glob"}.get(tool)
    if pattern_field:
        prefix = glob_literal_prefix(tool_input.get(pattern_field))
        if prefix:
            paths.append(prefix)
    return paths


def under(path, root):
    try:
        return os.path.commonpath([os.path.abspath(path), root]) == root
    except (ValueError, OSError):
        return False


def is_exempt_path_op(tool, tool_input, cwd):
    """True when every path the operation touches is under docs/PRDs or .aid,
    or is .claude/aid.json itself.

    An operation naming no path is unscoped and still counts, and one stray
    path outside the exempt dirs makes the whole op count.
    """
    paths = candidate_paths(tool, tool_input, cwd)
    if not paths:
        return False
    roots = [os.path.join(os.path.abspath(cwd), *parts) for parts in EXEMPT_DIRS]
    for path in paths:
        expanded = os.path.expanduser(path)
        absolute = expanded if os.path.isabs(expanded) else os.path.join(cwd, expanded)
        if not any(under(absolute, root) for root in roots):
            return False
    return True


def is_aid_cli(tool, tool_input):
    """True when every clause of a Bash command is an `aid` invocation."""
    if tool != "Bash":
        return False
    clauses = [c for c in CLAUSE_SPLIT.split(tool_input.get("command", "") or "")
               if c.strip()]
    return bool(clauses) and all(AID_CLI.match(c) for c in clauses)


def is_verification_op(tool, tool_input, settings):
    """True for a command that verifies or inspects rather than explores.

    The distinction the budget actually cares about is not how many calls have
    happened, it is whether answering the question requires ingesting
    substantially more text than the answer contains. A process check, a SHA
    comparison, `git show` on a known commit, a test run, or any search piped
    into `wc -l` all return an answer whose size is independent of what was
    scanned. Counting those measures activity, not cost.

    Bash only: the structured Read/Grep/Glob tools have no verification form
    (`Read` on a source file is exactly the bulk ingestion being policed).

    Exempting is the lax direction, so every rule here fails closed: a compound
    command qualifies only if *every* clause does, and a pager anywhere sinks
    the whole command.
    """
    if not settings.get("exempt_verification", True):
        return False
    if tool != "Bash":
        return False
    command = tool_input.get("command", "") or ""
    if not command.strip():
        return False
    # A counting pipe is verification regardless of what feeds it: the whole
    # point is that `grep -r ... | wc -l` yields one number, not a file dump.
    # Checked before the clause split, which would otherwise sever the pipe
    # from the search that feeds it.
    if COUNTING_PIPE.search(command):
        return True
    if PAGED.search(command):
        return False  # a listing that needs paging is longer than its answer
    clauses = [c for c in CLAUSE_SPLIT.split(command) if c.strip()]
    if not clauses:
        return False
    return all(
        GREP_COUNTING.search(clause)
        or PROC_READ.search(clause)
        or GIT_VERIFY.search(clause)
        or VERIFY_CMD.search(clause)
        or INSPECT_CMD.search(clause)
        for clause in clauses
    )


def main():
    payload = rc.read_payload()
    if payload is None:
        return 0
    if payload.get("agent_id") or payload.get("agent_type"):
        return 0  # subagent doing its job; never throttle workers

    tool = payload.get("tool_name", "")
    tool_input = rc.tool_input_of(payload)

    if tool in rc.AGENT_TOOLS:
        # A foreground dispatch cannot be raced, so it resets the budget; a
        # background one returns control at once, so the counter keeps climbing.
        if not tool_input.get("run_in_background", True):
            try:
                os.remove(counter_path(payload))
            except OSError:
                pass
        return 0
    if tool not in SEARCHY_TOOLS:
        if tool != "Bash":
            return 0
        command = tool_input.get("command", "") or ""
        if not SEARCHY_BASH.search(command):
            return 0
        if WRITE_REDIRECT.search(command):
            return 0  # writing a file is not exploration

    cwd = payload.get("cwd") if isinstance(payload.get("cwd"), str) else os.getcwd()
    if is_exempt_path_op(tool, tool_input, cwd) or is_aid_cli(tool, tool_input):
        return 0  # before the config load, so a broken aid.json can still be read
    config = rc.load(cwd)
    settings = rc.section(config, "guards")
    if is_verification_op(tool, tool_input, settings):
        return 0

    path = counter_path(payload)
    try:
        count = int(open(path).read())
    except Exception:
        count = 0
    count += 1
    try:
        with open(path, "w") as f:
            f.write(str(count))
    except OSError:
        return 0

    budget = rc.inline_budget(config)
    if count <= budget:
        return 0
    print(
        f"aid-guards: inline search op {count} this turn exceeds the budget of "
        f"{budget}. You are exploring inline at main-loop cost. Delegate the "
        "remaining discovery to aid-explorer in one Agent call; the budget "
        "resets when you delegate. Reads under docs/PRDs/ and .aid/, aid CLI "
        "calls, and verification commands (tests, linters, git log/show, "
        "process checks) are not counted. To raise the budget set "
        f'{{"guards": {{"inline_budget": {max(budget * 2, 20)}}}}} in '
        ".claude/aid.json; AID_GUARDS=off disables the guards for the session.",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    sys.exit(rc.run(main))
