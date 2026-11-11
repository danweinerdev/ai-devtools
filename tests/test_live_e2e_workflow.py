"""Guards the live suite's manual, disabled-by-default CI workflow.

The live suite makes real model calls, so `live-e2e.yml` must never start on a
push, a pull request or a schedule, must stay behind AID_LIVE_E2E, and must
never interpolate workflow inputs into a shell script (script injection).
These checks read the YAML as text: the repository has no PyYAML dependency.
"""

import re
import unittest
from pathlib import Path

WORKFLOW = Path(__file__).resolve().parent.parent / ".github" / "workflows" / "live-e2e.yml"


def read_lines():
    return WORKFLOW.read_text(encoding="utf-8").splitlines()


def top_level_keys(lines, section):
    """The keys directly under a top-level `section:` block."""
    keys = []
    inside = False
    for line in lines:
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        indent = len(line) - len(line.lstrip())
        if indent == 0:
            inside = line.rstrip() == f"{section}:"
            continue
        if inside and indent == 2:
            match = re.match(r"\s{2}([A-Za-z0-9_.-]+):", line)
            if match:
                keys.append(match.group(1))
    return keys


def jobs(lines):
    """Each job's name and its (indent, text) lines, keyed by name."""
    found = {}
    inside = False
    current = None
    for line in lines:
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        indent = len(line) - len(line.lstrip())
        if indent == 0:
            inside = line.rstrip() == "jobs:"
            current = None
            continue
        if not inside:
            continue
        if indent == 2:
            current = line.strip().rstrip(":")
            found[current] = []
        elif current is not None:
            found[current].append((indent, line.strip()))
    return found


def run_blocks(lines):
    """Every `run:` script's text, inline or block scalar."""
    blocks = []
    index = 0
    while index < len(lines):
        line = lines[index]
        match = re.match(r"^(\s*)run:\s*(.*)$", line)
        if not match:
            index += 1
            continue
        indent = len(match.group(1))
        rest = match.group(2).strip()
        if rest and not rest.startswith(("|", ">")):
            blocks.append(rest)
            index += 1
            continue
        body = []
        index += 1
        while index < len(lines):
            following = lines[index]
            if not following.strip():
                index += 1
                continue
            following_indent = len(following) - len(following.lstrip())
            if following_indent <= indent:
                break
            body.append(following.strip())
            index += 1
        blocks.append("\n".join(body))
    return blocks


class LiveE2EWorkflowTest(unittest.TestCase):
    def setUp(self):
        self.lines = read_lines()

    def test_only_workflow_dispatch_triggers_it(self):
        self.assertEqual(top_level_keys(self.lines, "on"), ["workflow_dispatch"])

    def test_every_job_is_gated_on_the_repository_variable(self):
        found = jobs(self.lines)
        self.assertTrue(found, "the workflow declares no jobs")
        for name, body in found.items():
            gates = [text for indent, text in body if indent == 4 and text.startswith("if:")]
            self.assertTrue(gates, f"job {name} has no if:")
            self.assertEqual(gates[0][len("if:"):].strip(), "vars.AID_LIVE_E2E == 'enabled'", name)

    def test_no_inputs_are_interpolated_into_run_scripts(self):
        blocks = run_blocks(self.lines)
        self.assertTrue(blocks, "the workflow has no run: steps")
        for block in blocks:
            self.assertNotIn("${{ inputs.", block)
            self.assertNotIn("${{ github.event.inputs.", block)


if __name__ == "__main__":
    unittest.main()
