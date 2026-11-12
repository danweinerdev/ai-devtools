---
name: aid-make-review
description: The Review stage of the Make loop, loaded by aid-make and not an entry point. Runs intent-isolated review lenses on their own read-only reviewers (aid-auditor for PRD compliance, aid-navigator for goal drift, aid-inspector for quality, aid-skeptic for fresh eyes, plus project reviewer agents) at the completion gates, re-runs every task's checks as one batch, and gives every finding an outcome. At the final gate, aid-sage reviews in its own context whether the governing PRD still describes what was built. Use the timing rules to decide when a review is due.
user-invocable: false
---

# Make, Review: independent eyes at the gates

**Loop:** make

## When to review

Reviews are saved for the moments they pay off: key completion gates and particularly complicated bodies of work. The unit of review is the feature, not the task: many small per-task reviews cost more and miss the overall context of what's changing.

1. **While iterating,** validate only what the requested change needs (aid-make-verify). No formal review; if it's unclear what the change needs, ask.
2. **Every feature is reviewed before it's approved,** at the `review-gate` task that closes it (aid-make-breakdown). The formal lenses also run when a PR is ready to be created, or when the user asks for a review.
3. **A risky task gets a single inspector.** When a task touches concurrency, security, data, or irreversible operations, or its builder raised something under `## Noticed`, dispatch aid-inspector on that task's diff alone, without asking. Its findings take the same outcomes as any other. A full review before the gate is still available for particularly complicated work: propose it, and the user approves.
4. **Project reviewer agents** run on completed features they're built for, alongside the built-in lenses. Their descriptions say what they cover.
5. **All four lenses run at a gate by default, and you may skip some.** Judge which ones a change needs. When no PRD governs the change, PRD compliance and goal drift have nothing to judge against, and are usually the first to go.

## The batch

Start with `aid check` (aid-graph): every task's checks, run together. It shows in one sweep that everything assembled is still green. Pass `--paths` with the changed files to limit it to the tasks a change touched. A task whose checks now fail is a finding.

## The lenses

Reviewer value comes from **intent isolation**: each lens gets only its inputs, so it can't forgive what the others would.

| Lens | Reviewer | Gets | Looks for |
|---|---|---|---|
| PRD compliance | aid-auditor | diff + governing PRD sections | every described behaviour present and correct; behaviour the PRD doesn't describe |
| Goal drift | aid-navigator | the PRD's overview and goals + the built result; no behaviour sections, breakdown, or graph | whether what was built, taken as a whole, achieves what the PRD set out to do |
| Quality | aid-inspector | diff + surrounding code, **no** PRD or brief | correctness, safety, maintainability, over-engineering, and **weakened proof** (below) |
| Fresh eyes | aid-skeptic | diff only | concrete failure scenarios: concurrency, retries, encoding, production failure, maintenance traps |
| Project reviewers | their own agent | what their own description asks for | their specialty |

**The quality lens's checklist.** aid-inspector's own prompt carries the eight classes (logic, edge cases, error handling, concurrency, resources, security, interface contracts, test quality), each with what it looks for and what it leaves to another class, the severity scale, the test-quality smells, and the verbatim-quote and dedupe rules. It stays inside the lens's inputs: the diff and the code only, with no PRD, brief, or plan.

- Each lens runs on its own reviewer: PRD compliance on aid-auditor, goal drift on aid-navigator, quality on aid-inspector, fresh eyes on aid-skeptic. aid-designer reviews UX and visual work. A builder role (aid-mechanic, aid-oracle, aid-creator, aid-craftsman) never reviews, its own work or anyone else's. Dispatch the lenses in parallel, and don't pre-read their inputs into the orchestrator, because that collapses independent views into one.
- A lens may be continued to add context or narrow it, within the inputs it's allowed to see. Intent isolation still rules: a lens is never continued from a builder's session.
- **Weakened proof** is a quality finding of its own kind: a check that passes without proving what it did. Look in the diff's tests and in any check commands for a test deleted, skipped, or marked expected-to-fail; an assertion removed, loosened (wider tolerance, weaker matcher, `any` where a value was checked), or rewritten to match whatever the code now outputs; a test narrowed until it no longer reaches the code it names; mocks that replace the very code under test; and a check command narrowed (a `-run` filter, a smaller package list, `|| true`, an exit code swallowed).
- Goal drift may run the feature or read its tests as well as the code: judging whether a goal was achieved often needs more than the diff.
- When reviews can't run in isolated contexts, run them one after another and **label the review as serial**. Don't present a single pass as an independent review.

## The final PRD review

The final completion gate has one more reader the lenses don't replace: **aid-sage**. It runs in its own context with the governing PRD and the built result (the diff and the code it touches), and nothing else: not the orchestrator's conclusion, the lenses' findings, the breakdown, the task graph, or the builders' reports. That isolation is the same rule as the lenses'. It checks that the PRD still describes what was built and that the architecture holds, and proposes fragments where either has drifted. It is not one of the lenses, runs in addition to them, and only at the last gate: route to it on the orchestrator's judgement, say why, and never as a default step.

## Synthesis

Start with `aid finish`'s list of tasks whose definition of done was weakened, if the work is at that gate, or `aid show` on the tasks reviewed: each weakening, with its reason, is put to the user alongside the lens findings.

Name every lens that was skipped, with its reason, so a thinner review is never presented as the full one. Findings that two or more lenses agree on are strong. Findings only one lens raised are listed separately, and fresh-eyes-only findings get particular attention. When a finding's claims are in doubt, the orchestrator can dispatch a blind aid-explorer to check them against the code. It gets the finding's file:line claims only, not the lens's reasoning, so it can't inherit the lens's conclusion. Every finding gets an outcome:

- **fixed:** a new or updated task in the graph, built and verified;
- **rejected,** with the reason;
- **deferred:** recorded as a `finding` state item (aid-state), resolved before the work finishes.

A **weakened-proof** finding is always put to the user, with the lens's evidence. The agent that built the task never resolves it, by fixing, rejecting, or deferring it on its own; the user decides which outcome it gets. That holds when the main loop did all the roles itself.
