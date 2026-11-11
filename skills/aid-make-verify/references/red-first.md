# Red first, and triage

A check is trusted only once it has been seen to fail for the right reason.

## The red run

When a task adds tests for behaviour the code doesn't have yet, run them on the **unchanged tree before the change is built**. Capture the command and the failing output in this session. That captured run is the proof the test can fail; it is evidence in the report, not a passing task check — the same command is attached to the task as its check, but before the build it is supposed to fail.

Classify every failure by **where** it fails and **why**:

| Observation | Classification | Action |
|---|---|---|
| The test loads, reaches its assertion, and fails because the described behaviour is missing | the expected red | Build the change. |
| The test doesn't load, import, compile, or is not collected | broken test | Fix the test before building; a check that can't run proves nothing. |
| The test fails in its own setup or fixture, or the failure message is a syntax, name, or type error in the test | broken test | Fix the test before building. |
| The test passes before the change | already-passing new check | Report it. The check is already attached to the task, so the orchestrator releases the task and the user decides whether to keep it (say, as a regression guard) or remove or replace it with `aid update-task <id> --check <command> --reason <why>` (aid-graph). It proves nothing about the change. |

An **assertion failure alone doesn't settle it.** A wrong expected value fails in exactly the same way as missing behaviour, so read the failure message and check that the assertion is the one you wrote for the described behaviour and that its expected value comes from the PRD or a spec the user owns — not from whatever the current code returns.

If a pre-change red run wasn't possible (for example, a test for existing behaviour that a refactor must preserve), show it failing for the right reason by temporarily breaking the behaviour, then restore it. If that isn't practical, report the check as done-unverified and record an open state item (aid-state); never present it as verified.

## Triage after the change

A check that fails after the change is a **source bug, a test bug, or the environment**, and it is a source bug until evidence says otherwise. That default is the point: the implementation is the thing under suspicion, not the test.

| Classification | Evidence that supports it | Action |
|---|---|---|
| Source bug | The test is the one that was red for this behaviour, and the failure is the described behaviour still missing or wrong | Fix the source, then run the check again. |
| Test bug | The test itself is wrong: a stale fixture, a race in the test's own setup, a typo in a helper, an expectation that doesn't match the criterion | Fix the test and say what was wrong. If the fix changes an expected value, the next section applies. |
| Environment | A missing tool, dependency, service, permission, port, or disk space; show the command, the error, and the environment fact | Fix or request the environment, rerun, and don't change code to satisfy it. |

**Expected values change only on specs the user owns.** A test's expected value may change when the PRD, or a spec the user owns, says the test is wrong. The implementation under test is never that evidence: "the code now returns X" and "verified from source" are not reasons to change an expected value. A failing contract test is never fixed by editing its assertion.

**Every such change is weakened proof and goes to the user at once.** Record the old value, the new value, and the PRD or user-owned spec that justifies it. The orchestrator takes it to the user immediately through aid-think-clarify, and records it for review as a state item (aid-state) or a weakened-proof finding (aid-make-review); it isn't deferred to the completion gate. The agent that built the task never resolves it, whether the main loop did all the roles itself or a specialist did. If the evidence doesn't clearly settle the classification, ask the orchestrator.

The red run and its triage belong in the builder's report; the task's checks are the commands that must pass afterwards, run through `aid complete-task` (aid-graph).
