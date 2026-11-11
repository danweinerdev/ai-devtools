---
name: aid-make-verify
description: The Verify stage of the Make loop, loaded by aid-make and not an entry point. Proves a task with captured evidence: while iterating it validates only what the change needs, and a task is marked done only when the task tool has run its checks and they pass. Covers red-first proof and triage, and never editing a contract test's assertion to make it pass.
user-invocable: false
---

# Make, Verify: captured evidence

**Loop:** make

A task is done only when its verification output has been **captured and shown**: the command run and the output observed in this session. "Should pass" and "passed earlier" don't count.

- **Validate only what the change needs.** While iterating, run the checks that prove this change works and nothing more. Full suites and formal review wait for the completion gates (aid-make-review). If it's unclear what the change needs, find the command in the repository's evidence (aid-make-checks); if that doesn't settle it, the orchestrator asks the user, and a builder stops and reports it.
- **Complete through the tool, from the main loop.** The orchestrator runs `aid complete-task <id> --holder <role>` (the role it claimed the task for) once the builder reports; it re-runs the task's checks and marks it `done` only if they pass (aid-graph). Show its output. Builders run the brief's check commands and report the output; they never mark their own task done.
- **Red first, then prove a test can fail.** When a task adds tests for behaviour the code doesn't have, run them on the unchanged tree before building and capture the failure. Classify each failure before trusting the test: an assertion failure alone doesn't settle it, because a wrong expected value fails the same way (references/red-first.md). A broken test is fixed before building. A new check that already passes is reported to the orchestrator: it is already attached to the task, so the orchestrator releases the task and, once the user decides, keeps it (say, as a regression guard) or removes or replaces it with `aid update-task <id> --check <command> --reason <why>`; the `--check` list replaces the whole list, so repeat it for each check kept (aid-graph). It proves nothing about the change and is never counted as its proof. If a pre-change red run wasn't possible — a test for existing behaviour — break the behaviour once and watch it fail; if that isn't practical, report the check as done-unverified and record an open state item (aid-state), never as verified.
- **Triage a post-change failure.** It's a source bug until evidence says otherwise. A failing contract test is never fixed by editing the assertion: an expected value changes only when the PRD or a spec the user owns says the test is wrong, never because the implementation now outputs something else. Every such change is weakened proof: the orchestrator takes it to the user at once (aid-think-clarify) and records it for review (aid-state, aid-make-review), not deferred to the completion gate.
- **Report honestly:** done and verified, done but unverified (say why), partial, or blocked. A blocked task is marked with `aid block-task` and a reason.

When the ready frontier is empty and every task is done, the feature is complete: hand to aid-make-review.
