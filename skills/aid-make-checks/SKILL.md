---
name: aid-make-checks
description: The Checks stage of the Make loop, loaded by aid-make and not an entry point. Finds a task's test and lint commands from the repository's evidence — test configuration, then dependency manifests, then the build system, then imports — with the command each marker implies, where each language keeps its tests, and the mapping from every acceptance criterion to a check, asking instead of guessing when the evidence doesn't settle it. Use it when writing a plan's checks or when it's unclear what to run.
user-invocable: false
---

# Make, Checks: the project's own commands

**Loop:** make

A task's checks are the project's own test and lint commands, found from evidence in the repository, never guessed. The same stage settles where a new test goes: beside the project's existing tests of the same kind, never outside the project's test paths.

1. **Find the command.** Follow the evidence order in [references/finding-checks.md](references/finding-checks.md): test configuration, then dependency manifests, then the build system, then imports. The reference carries the marker-file → command table for test and lint, the check-only forms with no auto-fix, and how to narrow to one test, file, or package. When the evidence doesn't settle a command — two runners configured, a script that wraps something unrecognisable, no test configuration at all — the orchestrator asks the user instead of guessing. A builder that hits an unsettled command stops and reports it; it doesn't choose one.
2. **Put the test where the project keeps them.** [references/test-locations.md](references/test-locations.md) lists the conventional paths per language. Existing tests win over the convention: mirror the layout, and never create a test outside the project's test paths.
3. **Map every criterion to a check.** [references/traceability.md](references/traceability.md): the breakdown lists each acceptance criterion of the PRD sections in scope against at least one task's check. A criterion with no check is recorded as an open state item straight away (aid-state), so the work can't finish while it's unproven.

Checks are commands that run non-interactively from the repository root and pass or fail by exit code. The rules for writing them — one named test per check where practical, never interactive, never dependent on the current branch or commit — live in aid-graph's "Writing good checks" ([../aid-graph/SKILL.md](../aid-graph/SKILL.md)). Read them before attaching a check to a task.
