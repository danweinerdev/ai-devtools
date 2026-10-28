---
name: aid-make-review
description: The Review stage of the Make loop, loaded by aid-make and not an entry point. Runs intent-isolated review lenses (PRD compliance, goal drift, quality, fresh eyes, plus project reviewer agents) at the completion gates, re-runs every task's checks as one batch, and gives every finding an outcome. Use the timing rules to decide when a review is due.
user-invocable: false
---

# Make, Review: independent eyes at the gates

**Loop:** make

## When to review

Reviews are saved for the moments they pay off. The unit of review is the feature, not the task: many small per-task reviews cost more and miss the overall context of what's changing.

1. **While iterating,** validate only what the change needs (aid-make-verify). No formal review.
2. **At the completion gates** the formal lenses always run: when a feature is complete, when a PR is ready to be created, or when the user asks for a review.
3. **Complicated or high-risk work** earns a review before the gate. Propose one when the work warrants it; the user approves.
4. **Project reviewer agents** run on completed features they're built for, alongside the built-in lenses.

## The batch

Start with `aid check` (aid-graph): every task's checks, run together. It shows in one sweep that everything assembled is still green. Pass `--paths` with the changed files to limit it to the tasks a change touched. A task whose checks now fail is a finding.

## The lenses

Reviewer value comes from **intent isolation**: each lens gets only its inputs, so it can't forgive what the others would.

| Lens | Gets | Looks for |
|---|---|---|
| PRD compliance | diff + governing PRD sections | every described behaviour present and correct; behaviour the PRD doesn't describe |
| Goal drift | the PRD's overview and goals + the built result; no behaviour sections, breakdown, or graph | whether what was built, taken as a whole, achieves what the PRD set out to do |
| Quality | diff + surrounding code, **no** PRD or brief | correctness, safety, maintainability, over-engineering |
| Fresh eyes | diff only | concrete failure scenarios: concurrency, retries, encoding, production failure, maintenance traps |
| Project reviewers | what their own description asks for | their specialty |

- aid-oracle is the default reviewer for the built-in lenses. aid-designer reviews UX and visual work. Dispatch the lenses in parallel, and don't pre-read their inputs into the orchestrator, because that collapses independent views into one.
- Goal drift may run the feature or read its tests as well as the code: judging whether a goal was achieved often needs more than the diff.
- When reviews can't run in isolated contexts, run them one after another and **label the review as serial**. Don't present a single pass as an independent review.

## Synthesis

Findings that two or more lenses agree on are strong. Findings only one lens raised are listed separately, and fresh-eyes-only findings get particular attention. Every finding gets an outcome:

- **fixed:** a new or updated task in the graph, built and verified;
- **rejected,** with the reason;
- **deferred:** recorded as a `finding` state item (aid-state), resolved before the work finishes.
