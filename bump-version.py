#!/usr/bin/env python3
"""Bump the plugin version, regenerate, commit and tag it.

Usage: bump-version.py <patch|minor|major>

The version lives in package.json, .claude-plugin/plugin.json and
.codex-plugin/plugin.json (AGENTS.md, Versioning), and all three move together.
A bump needs a clean work tree, so the tagged sources are the ones the gate
checked. The bump is gated: the whole suite runs before anything is written,
`make build` regenerates the artifacts, and a second `make check` proves the
result. Only then is vX.Y.Z committed and tagged. Nothing is ever pushed.
"""

import json
import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent

MANIFESTS = (
    ROOT / "package.json",
    ROOT / ".claude-plugin" / "plugin.json",
    ROOT / ".codex-plugin" / "plugin.json",
)

# Committed with the bump: the three manifests, plus the artifacts `make build`
# regenerates. On a clean tree the build is a no-op, and `make check` after it
# fails when anything is left stale.
COMMIT_PATHS = (
    "package.json",
    ".claude-plugin/plugin.json",
    ".codex-plugin/plugin.json",
    "skills-manifest.json",
    "agents-manifest.json",
    "dist",
)

# An outer make exports these into recipes, and a nested make that inherits them
# would warn about a jobserver it cannot use.
MAKE_ENV_DROP = ("MAKEFLAGS", "MFLAGS", "MAKELEVEL")


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    sys.exit(1)


def step(message: str) -> None:
    print(f"==> {message}", file=sys.stderr)


# All file IO pins encoding and newlines explicitly: the defaults use the
# platform locale encoding and translate "\n" to os.linesep, which churns
# committed files on Windows.


def read_utf8(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def write_utf8(path: Path, content: str) -> None:
    with path.open("w", encoding="utf-8", newline="\n") as f:
        f.write(content)


# Canonical semver core only: no signs, no spaces, no leading zeros. Anything
# looser ("1.2", "01.2.3", "1.2.3 ") would be committed as the version, and
# harnesses resolve caches by parsing it.
VERSION_RE = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")


def parse(version: str) -> tuple[int, int, int]:
    match = VERSION_RE.fullmatch(version)
    if match is None:
        fail(f"{version!r} is not a canonical X.Y.Z version (no signs, spaces or leading zeros)")
    return int(match.group(1)), int(match.group(2)), int(match.group(3))


def bump(version: str, part: str) -> str:
    major, minor, patch = parse(version)
    if part == "major":
        return f"{major + 1}.0.0"
    if part == "minor":
        return f"{major}.{minor + 1}.0"
    return f"{major}.{minor}.{patch + 1}"


def current_version() -> str:
    """The version the three manifests agree on; a mismatch is a hard stop."""
    versions: dict[Path, str] = {}
    for path in MANIFESTS:
        relative = path.relative_to(ROOT)
        if not path.is_file():
            fail(f"{relative} is missing")
        try:
            data = json.loads(read_utf8(path))
        except json.JSONDecodeError as error:
            fail(f"{relative} is not valid JSON: {error}")
        version = data.get("version")
        if not isinstance(version, str):
            fail(f"{relative} has no version")
        parse(version)
        versions[path] = version

    if len(set(versions.values())) != 1:
        listed = ", ".join(f"{p.relative_to(ROOT)} at {v}" for p, v in versions.items())
        fail(f"the manifests disagree on the version ({listed}); make them agree first")
    return versions[MANIFESTS[0]]


def version_pattern(version: str) -> "re.Pattern[str]":
    # Only the top-level version value moves: rewriting the parsed JSON would
    # reformat package.json (its keywords array is one line) for no reason.
    return re.compile(r'("version"\s*:\s*")' + re.escape(version) + '"')


def plan_writes(version: str, new_version: str) -> tuple[dict[Path, str], dict[Path, str]]:
    """Work out every write before making any, so a surprise writes nothing."""
    originals: dict[Path, str] = {}
    updates: dict[Path, str] = {}
    for path in MANIFESTS:
        text = read_utf8(path)
        updated, count = version_pattern(version).subn(
            lambda match: f'{match.group(1)}{new_version}"', text
        )
        if count != 1:
            fail(
                f'{path.relative_to(ROOT)}: expected exactly one "version": "{version}", '
                f"found {count}"
            )
        originals[path] = text
        updates[path] = updated
    return originals, updates


def commit_paths() -> list[str]:
    return [path for path in COMMIT_PATHS if (ROOT / path).exists()]


def git(*args: str) -> subprocess.CompletedProcess:
    return subprocess.run(["git", *args], cwd=ROOT, capture_output=True, text=True)


def preflight(version: str) -> None:
    inside = git("rev-parse", "--is-inside-work-tree")
    if inside.returncode != 0 or inside.stdout.strip() != "true":
        fail(f"{ROOT} is not inside a git work tree; the bump commits and tags, so it needs one")

    # A clean tree, ignored files aside, is what makes the commit honest: the
    # sources that were checked are the sources that get tagged. It also keeps
    # the rollback safe, since there is no staged-only content to lose. `git
    # status` leaves out ignored paths, and nothing else is exempt: staged
    # changes, partial staging and untracked files all refuse, including ones
    # to the manifests and generated artifacts. `--untracked-files=all` is
    # explicit because `status.showUntrackedFiles=no` would otherwise hide
    # untracked sources from this check.
    status = git("status", "--porcelain", "--untracked-files=all")
    if status.returncode != 0:
        fail(f"git status failed: {status.stderr.strip()}")
    if status.stdout.strip():
        listed = "\n".join("  " + line for line in status.stdout.rstrip().splitlines())
        fail(
            "refusing to bump: the work tree is not clean, so the tagged sources "
            "would not be the ones make check proved. Commit, stash or discard "
            f"these first:\n{listed}"
        )

    tag = f"v{version}"
    if git("rev-parse", "-q", "--verify", f"refs/tags/{tag}").returncode == 0:
        fail(f"tag {tag} already exists; nothing was written, committed or tagged")


def run_make(target: str) -> bool:
    step(f"make {target}")
    env = {key: value for key, value in os.environ.items() if key not in MAKE_ENV_DROP}
    return subprocess.run(["make", target], cwd=ROOT, env=env).returncode == 0


def restore(originals: dict[Path, str]) -> None:
    for path, text in originals.items():
        write_utf8(path, text)
    # Best effort: leave nothing of ours staged. The clean-tree preflight means
    # a HEAD always exists here, but a rollback should not depend on it.
    if git("rev-parse", "-q", "--verify", "HEAD").returncode == 0:
        subprocess.run(
            ["git", "reset", "-q", "--", *commit_paths()], cwd=ROOT, capture_output=True
        )


def usage() -> None:
    print(
        "Usage: bump-version.py <patch|minor|major>\n"
        "\n"
        "Bumps package.json, .claude-plugin/plugin.json and .codex-plugin/plugin.json,\n"
        "runs `make build` and `make check`, then commits vX.Y.Z and tags it. Never pushes.",
        file=sys.stderr,
    )
    sys.exit(1)


def main() -> None:
    args = sys.argv[1:]
    if len(args) != 1 or args[0] not in ("patch", "minor", "major"):
        usage()
    part = args[0]

    current = current_version()
    new_version = bump(current, part)
    preflight(new_version)

    # The gate runs before anything is written, so a red tree leaves no bump.
    if not run_make("check"):
        fail("make check failed; nothing was written, committed or tagged")

    originals, updates = plan_writes(current, new_version)
    for path, text in updates.items():
        write_utf8(path, text)
    step(f"bumped {current} to {new_version} in all three manifests")

    for target in ("build", "check"):
        if not run_make(target):
            restore(originals)
            fail(
                f"make {target} failed after the bump; restored the manifests, "
                "committed nothing and tagged nothing"
            )

    tag = f"v{new_version}"
    added = git("add", "--", *commit_paths())
    if added.returncode != 0:
        restore(originals)
        fail(f"git add failed: {added.stderr.strip()}")

    step(f"committing {tag}")
    committed = git("commit", "-m", tag)
    if committed.returncode != 0:
        restore(originals)
        fail(f"git commit failed: {committed.stderr.strip()}")

    step(f"tagging {tag}")
    tagged = git("tag", "-a", tag, "-m", tag)
    if tagged.returncode != 0:
        fail(f"committed {tag} but git tag failed: {tagged.stderr.strip()}")

    print(new_version)


if __name__ == "__main__":
    main()
