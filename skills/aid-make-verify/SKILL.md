---
name: aid-make-verify
description: The Verify stage of the Make loop, loaded by aid-make and not an entry point. Proves a task with captured evidence: while iterating it validates only what the change needs, and a task is marked done only when the task tool has run its checks and they pass. Covers proving new tests can fail and never editing a contract test's assertion to make it pass.
user-invocable: false
---

# Make, Verify: captured evidence

**Loop:** make

A task is done only when its verification output has been **captured and shown**: the command run and the output observed in this session. "Should pass" and "passed earlier" don't count.

- **Validate only what the change needs.** While iterating, run the checks that prove this change works and nothing more. Full suites and formal review wait for the completion gates (aid-make-review). If it's unclear what the change needs, ask.
- **Complete through the tool.** `aid complete-task <id> --holder <role>` runs the task's checks and marks it `done` only if they pass (aid-graph). Show its output.
- **Prove new tests can fail.** A test you've only ever seen pass may test nothing, so break the behaviour once or justify why that isn't needed.
- **A failing contract test is never fixed by editing the assertion.** Assume the code is wrong until evidence says the test is.
- **Report honestly:** done and verified, done but unverified (say why), partial, or blocked. A blocked task is marked with `aid block-task` and a reason.

When the ready frontier is empty and every task is done, the feature is complete: hand to aid-make-review.
