"""End-to-end tests for bump-version.py.

Every test drives the real script inside a throwaway git repository, so
committing and tagging are exercised without cutting a release in this
checkout. The throwaway repo's Makefile supplies `build` and `check`, which is
how a failing regeneration or a red gate is simulated.
"""

import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SCRIPT = REPO / "bump-version.py"
MANIFESTS = (
    "package.json",
    ".claude-plugin/plugin.json",
    ".codex-plugin/plugin.json",
)


def python(code: str) -> str:
    """A Makefile recipe that runs `code` with the interpreter running the tests."""
    return f'"$(PYTHON)" -c "{code}"'


class ReleaseFlowTest(unittest.TestCase):
    def setUp(self) -> None:
        self.root = Path(tempfile.mkdtemp(prefix="bump-version-test-"))
        self.addCleanup(shutil.rmtree, self.root, ignore_errors=True)
        self.env = {
            **os.environ,
            # Keep the tests off any real identity, signing or hook setup.
            "GIT_AUTHOR_NAME": "release test",
            "GIT_AUTHOR_EMAIL": "release@example.com",
            "GIT_COMMITTER_NAME": "release test",
            "GIT_COMMITTER_EMAIL": "release@example.com",
            "GIT_CONFIG_GLOBAL": os.devnull,
            "GIT_CONFIG_NOSYSTEM": "1",
            "GIT_TERMINAL_PROMPT": "0",
        }
        for key in ("MAKEFLAGS", "MFLAGS", "MAKELEVEL"):
            self.env.pop(key, None)

    def new_repo(
        self,
        version: str = "0.1.0",
        *,
        build: str | None = None,
        check: str | None = None,
        init: bool = True,
        gitignore: str | None = None,
    ) -> None:
        for entry in self.root.iterdir():
            if entry.is_dir():
                shutil.rmtree(entry)
            else:
                entry.unlink()
        shutil.copy2(SCRIPT, self.root / "bump-version.py")
        self.write_manifests(version)
        self.write_makefile(build, check)
        if gitignore is not None:
            (self.root / ".gitignore").write_text(gitignore, encoding="utf-8")
        if init:
            self.git("init", "-q")
            self.git("add", "-A")
            committed = self.git("commit", "-q", "-m", "initial")
            self.assertEqual(committed.returncode, 0, committed.stderr)

    def write_manifests(self, version: str) -> None:
        """The repo's real manifests at `version`, so formatting stays realistic."""
        encoded = json.dumps(version)[1:-1]  # JSON-escape it: a raw newline is invalid JSON
        for relative in MANIFESTS:
            source = (REPO / relative).read_text(encoding="utf-8")
            current = json.loads(source)["version"]
            updated, count = re.subn(
                r'("version"\s*:\s*")' + re.escape(current) + '"',
                lambda match: f'{match.group(1)}{encoded}"',
                source,
                count=1,
            )
            self.assertEqual(count, 1, f"{relative}: no version to replace")
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(updated, encoding="utf-8")

    def write_makefile(self, build: str | None, check: str | None) -> None:
        build = build or python(
            "import pathlib; pathlib.Path('build.stamp').write_text('built')"
        )
        check = check or python("import sys; sys.exit(0)")
        (self.root / "Makefile").write_text(
            f"PYTHON := {sys.executable}\n\nbuild:\n\t@{build}\n\ncheck:\n\t@{check}\n",
            encoding="utf-8",
        )

    def git(self, *args: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            ["git", *args], cwd=self.root, capture_output=True, text=True, env=self.env
        )

    def run_bump(self, part: str = "patch") -> subprocess.CompletedProcess:
        return subprocess.run(
            [sys.executable, "bump-version.py", part],
            cwd=self.root,
            capture_output=True,
            text=True,
            env=self.env,
        )

    def version_in(self, relative: str) -> str:
        return json.loads((self.root / relative).read_text(encoding="utf-8"))["version"]

    def text_in(self, relative: str) -> str:
        return (self.root / relative).read_text(encoding="utf-8")

    def head(self) -> str:
        return self.git("rev-parse", "HEAD").stdout.strip()

    def tags(self) -> list[str]:
        return self.git("tag", "-l").stdout.split()

    def assert_untouched(self, version: str, head: str) -> None:
        for relative in MANIFESTS:
            self.assertEqual(self.version_in(relative), version, relative)
        self.assertEqual(self.head(), head)
        self.assertEqual(self.tags(), [])

    def test_every_increment_and_its_reset(self) -> None:
        cases = (
            ("0.1.0", "patch", "0.1.1"),
            ("0.1.0", "minor", "0.2.0"),
            ("0.1.0", "major", "1.0.0"),
            ("1.2.3", "patch", "1.2.4"),
            ("1.2.3", "minor", "1.3.0"),
            ("1.2.3", "major", "2.0.0"),
            ("0.9.9", "patch", "0.9.10"),
        )
        for start, part, expected in cases:
            with self.subTest(start=start, part=part):
                self.new_repo(start)
                result = self.run_bump(part)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.strip(), expected)
                for relative in MANIFESTS:
                    self.assertEqual(self.version_in(relative), expected, relative)

    def test_all_three_manifests_move_together_and_nothing_else_does(self) -> None:
        self.new_repo("0.1.0")
        before = {relative: self.text_in(relative) for relative in MANIFESTS}

        result = self.run_bump("patch")
        self.assertEqual(result.returncode, 0, result.stderr)

        for relative in MANIFESTS:
            expected, count = re.subn(
                r'("version"\s*:\s*")0\.1\.0"',
                r'\g<1>0.1.1"',
                before[relative],
                count=1,
            )
            self.assertEqual(count, 1, relative)
            self.assertEqual(self.text_in(relative), expected, relative)

        # package.json's one-line keywords array survives: no JSON reformatting.
        self.assertIn(
            '"keywords": ["opencode-plugin", "claude-code-plugin"',
            self.text_in("package.json"),
        )

    def test_failed_gate_changes_nothing(self) -> None:
        self.new_repo(check=python("import sys; sys.exit(1)"))
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("make check failed", result.stderr)
        self.assert_untouched("0.1.0", head)
        self.assertEqual(self.git("status", "--porcelain").stdout.strip(), "")
        # The gate comes before the bump, so nothing was even regenerated.
        self.assertFalse((self.root / "build.stamp").exists())

    def test_regeneration_failure_commits_and_tags_nothing(self) -> None:
        self.new_repo(build=python("import sys; sys.exit(1)"))
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("make build failed", result.stderr)
        self.assert_untouched("0.1.0", head)
        # The bumped manifests are rolled back, so a failed release leaves a clean tree.
        self.assertEqual(self.git("status", "--porcelain").stdout.strip(), "")

    def test_normal_bump_commits_and_creates_an_annotated_tag(self) -> None:
        self.new_repo(
            build=python(
                "import pathlib; pathlib.Path('skills-manifest.json').write_text('regenerated')"
            )
        )

        result = self.run_bump("patch")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "0.1.1")
        self.assertEqual(self.git("log", "-1", "--pretty=%s").stdout.strip(), "v0.1.1")
        self.assertEqual(self.tags(), ["v0.1.1"])
        self.assertEqual(self.git("cat-file", "-t", "v0.1.1").stdout.strip(), "tag")
        self.assertEqual(self.git("rev-list", "-n", "1", "v0.1.1").stdout.strip(), self.head())

        committed = self.git("show", "--name-only", "--pretty=format:", "HEAD").stdout.split()
        for relative in MANIFESTS:
            self.assertIn(relative, committed)
        # The regenerated artifact lands in the same commit.
        self.assertIn("skills-manifest.json", committed)
        self.assertEqual(self.git("status", "--porcelain").stdout.strip(), "")

    def test_bump_failure_on_a_non_canonical_version(self) -> None:
        invalid = (
            "1.2",
            "1.2.3.4",
            "v1.2.3",
            "1.2.3-rc.1",
            "+1.2.3",
            "-1.2.3",
            " 1.2.3",
            "1.2.3 ",
            "1.2.3\n",
            "01.2.3",
            "1.02.3",
            "1.2.03",
            "1.-2.3",
            "1.2.x",
        )
        for version in invalid:
            with self.subTest(version=version):
                self.new_repo(version)
                head = self.head()

                result = self.run_bump("patch")

                self.assertNotEqual(result.returncode, 0)
                self.assertIn("is not a canonical X.Y.Z version", result.stderr)
                self.assert_untouched(version, head)
                self.assertEqual(self.git("status", "--porcelain").stdout.strip(), "")

    def test_bump_failure_when_the_manifests_disagree(self) -> None:
        self.new_repo("0.1.0")
        path = self.root / ".claude-plugin/plugin.json"
        text, count = re.subn(
            r'"version": "0\.1\.0"', '"version": "0.2.0"', self.text_in(".claude-plugin/plugin.json"), count=1
        )
        self.assertEqual(count, 1)
        path.write_text(text, encoding="utf-8")
        head = self.git("rev-parse", "HEAD").stdout.strip()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("disagree on the version", result.stderr)
        self.assertEqual(self.head(), head)
        self.assertEqual(self.tags(), [])
        self.assertEqual(self.version_in("package.json"), "0.1.0")

    def test_unrelated_staged_changes_are_refused(self) -> None:
        self.new_repo("0.1.0")
        notes = self.root / "notes.txt"
        notes.write_text("first\n", encoding="utf-8")
        self.git("add", "-A")
        self.assertEqual(self.git("commit", "-q", "-m", "notes").returncode, 0)
        notes.write_text("second\n", encoding="utf-8")
        self.git("add", "notes.txt")
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing to bump", result.stderr)
        self.assertIn("notes.txt", result.stderr)
        self.assert_untouched("0.1.0", head)
        # The unrelated change stays staged, exactly where the maintainer left it.
        self.assertEqual(self.git("diff", "--cached", "--name-only").stdout.strip(), "notes.txt")
        self.assertEqual(notes.read_text(encoding="utf-8"), "second\n")

    def test_unstaged_tracked_changes_are_refused(self) -> None:
        self.new_repo("0.1.0")
        notes = self.root / "notes.txt"
        notes.write_text("first\n", encoding="utf-8")
        self.git("add", "-A")
        self.assertEqual(self.git("commit", "-q", "-m", "notes").returncode, 0)
        notes.write_text("edited\n", encoding="utf-8")
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("the work tree is not clean", result.stderr)
        self.assertIn("notes.txt", result.stderr)
        self.assert_untouched("0.1.0", head)
        self.assertEqual(notes.read_text(encoding="utf-8"), "edited\n")
        self.assertEqual(self.git("diff", "--cached", "--name-only").stdout.strip(), "")

    def test_untracked_files_are_refused(self) -> None:
        self.new_repo("0.1.0")
        stray = self.root / "notes.txt"
        stray.write_text("draft\n", encoding="utf-8")
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("the work tree is not clean", result.stderr)
        self.assertIn("notes.txt", result.stderr)
        self.assert_untouched("0.1.0", head)
        self.assertEqual(stray.read_text(encoding="utf-8"), "draft\n")
        self.assertEqual(self.git("status", "--porcelain").stdout.strip(), "?? notes.txt")

    def test_untracked_files_are_refused_even_when_status_hides_them(self) -> None:
        self.new_repo("0.1.0")
        # A maintainer's config can hide untracked files from plain git status.
        self.assertEqual(self.git("config", "status.showUntrackedFiles", "no").returncode, 0)
        stray = self.root / "notes.txt"
        stray.write_text("draft\n", encoding="utf-8")
        self.assertEqual(self.git("status", "--porcelain").stdout.strip(), "")
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("the work tree is not clean", result.stderr)
        self.assertIn("notes.txt", result.stderr)
        self.assert_untouched("0.1.0", head)
        self.assertEqual(stray.read_text(encoding="utf-8"), "draft\n")

    def test_staged_and_partially_staged_manifest_content_survives_refusal(self) -> None:
        self.new_repo("0.1.0")
        path = self.root / "package.json"
        original = self.text_in("package.json")
        staged = original.replace('"description": "Think', '"description": "STAGED Think', 1)
        self.assertNotEqual(staged, original, "fixture has no description to edit")
        path.write_text(staged, encoding="utf-8")
        self.assertEqual(self.git("add", "package.json").returncode, 0)
        partial = staged.replace('"license": "MIT"', '"license": "MIT-0"', 1)
        self.assertNotEqual(partial, staged, "fixture has no license to edit")
        path.write_text(partial, encoding="utf-8")
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("the work tree is not clean", result.stderr)
        self.assertIn("package.json", result.stderr)
        self.assert_untouched("0.1.0", head)
        # Nothing of the maintainer's is lost: the index still holds the staged
        # blob rather than the worktree version, and the worktree edit is intact.
        self.assertEqual(self.git("show", ":package.json").stdout, staged)
        self.assertEqual(self.text_in("package.json"), partial)

    def test_staged_generated_artifact_is_refused_and_preserved(self) -> None:
        self.new_repo("0.1.0")
        artifact = self.root / "skills-manifest.json"
        artifact.write_text("original\n", encoding="utf-8")
        self.git("add", "-A")
        self.assertEqual(self.git("commit", "-q", "-m", "artifact").returncode, 0)
        artifact.write_text("staged\n", encoding="utf-8")
        self.assertEqual(self.git("add", "skills-manifest.json").returncode, 0)
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("the work tree is not clean", result.stderr)
        self.assertIn("skills-manifest.json", result.stderr)
        self.assert_untouched("0.1.0", head)
        self.assertEqual(self.git("show", ":skills-manifest.json").stdout, "staged\n")
        self.assertEqual(artifact.read_text(encoding="utf-8"), "staged\n")

    def test_ignored_files_do_not_refuse_a_bump(self) -> None:
        self.new_repo("0.1.0", gitignore="*.log\n/build/\n")
        (self.root / "build").mkdir()
        log = self.root / "build" / "aid.log"
        log.write_text("noise\n", encoding="utf-8")

        result = self.run_bump("patch")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.version_in("package.json"), "0.1.1")
        self.assertEqual(self.tags(), ["v0.1.1"])
        self.assertEqual(log.read_text(encoding="utf-8"), "noise\n")

    def test_existing_tag_is_refused(self) -> None:
        self.new_repo("0.1.0")
        self.git("tag", "v0.1.1")
        head = self.head()

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("already exists", result.stderr)
        for relative in MANIFESTS:
            self.assertEqual(self.version_in(relative), "0.1.0", relative)
        self.assertEqual(self.head(), head)
        # The tag it collided with is left exactly where it was.
        self.assertEqual(self.tags(), ["v0.1.1"])
        self.assertEqual(self.git("rev-parse", "v0.1.1").stdout.strip(), head)

    def test_outside_a_git_work_tree_is_refused(self) -> None:
        self.new_repo("0.1.0", init=False)

        result = self.run_bump("patch")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("not inside a git work tree", result.stderr)
        for relative in MANIFESTS:
            self.assertEqual(self.version_in(relative), "0.1.0", relative)

    def test_unknown_part_is_a_usage_error(self) -> None:
        self.new_repo("0.1.0")
        head = self.head()

        result = self.run_bump("sideways")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Usage: bump-version.py", result.stderr)
        self.assert_untouched("0.1.0", head)


if __name__ == "__main__":
    unittest.main()
