---
name: aid-make-delegate
description: The Delegate stage of the Make loop, loaded by aid-make and not an entry point. Picks ready tasks from the task graph, claims them, and routes each to the cheapest aid-* role or project agent that can succeed, with a complete brief. Independent tasks go out in parallel.
user-invocable: false
---

# Make, Delegate: claim, route, brief

**Loop:** make

1. **Pick.** Take tasks from the ready frontier (`aid ready`). Tasks whose footprints don't overlap can run in parallel; overlapping ones run in sequence.
2. **Claim.** `aid claim-task <id> --holder <role>`, so no two builders take the same task (aid-graph). Once the harness returns the builder's session ID, claim again with `--holder <role> --session <id>`: that records the session on the task, where it survives compaction, and renews the lease.
3. **Route** to the cheapest role that can succeed:

| Role | Use for | Not for |
|---|---|---|
| aid-explorer | research, locating, comparing options, reading branches and references | deciding |
| aid-mechanic | well-specified technical building: features from a clear spec, tests, wiring, refactors | open design questions |
| aid-oracle | high-risk or novel implementation (concurrency, security, data migration, core algorithms); builds, never reviews | routine work, where it's wasted, and any review |
| aid-auditor | PRD compliance review, and the independent PRD-against-code judgement | building |
| aid-navigator | goal drift review | building |
| aid-inspector | quality review, with the bug-class checklist and weakened proof; a single risky task's diff before its gate | building |
| aid-skeptic | fresh eyes review | building |
| aid-sage | cross-project architecture, a problem another specialist reported it couldn't settle, and the final-gate PRD review | one project's contained architecture (aid-oracle's), and routine work |
| aid-designer | visual and UX direction: flows, layout, interaction, visual language | writing production code |
| aid-creator | first-pass creative work: drafts, copy, prototypes, sketches, first versions | final polish |
| aid-craftsman | complex refinement: turning a working first pass into a finished one, hard polish, deep cleanup | greenfield first passes |

Project agents are routing targets too, when their description fits the task better than a built-in role. Models are configured per harness; don't name models in briefs.

Review lenses go to their reviewers, and the `review-gate` task that closes a feature is held by the orchestrator, which runs the review itself (aid-make-review): it's never delegated to a builder.

Route to aid-sage sparingly, on the orchestrator's judgement, never as a default step, and say why. A question that crosses more than one project is its; one contained in a project is aid-oracle's. So is a problem another specialist reported it couldn't settle, and the PRD review at the final completion gate.

## The brief

A subagent knows only its brief. Include:

- the goal, and the task's ID;
- the governing PRD sections, by path and heading;
- the footprint: the files in scope;
- the definition of done and the exact check commands that must pass;
- when the task adds tests for new behaviour, the red run it must produce: the tests run on the unchanged tree before the build, with the captured failure and its classification (aid-make-verify, references/red-first.md);
- what's already been ruled out;
- the report shape, including `## PRD changes proposed`, anything noticed that isn't part of the task, and, when the task added tests, the captured red run with its classification;
- for a task that depends on another, the predecessor's validated results (findings, changed files, commit IDs), never its raw report.

Prefer continuing a builder that already holds the task's context (pass its session id) over starting a new one, and continue a session only as the role it was first dispatched as. Tell it what changed, and start fresh when old context would mislead or bloat it (aid-orchestrate). When resending a task (corrections, or review findings on a done task), take its builder's session from the task (`aid show <id>`) rather than from memory, and start fresh when the harness can't resume it.

Before redispatching a failed, cancelled, or timed-out builder, confirm it has stopped and inspect what it already changed. Never run a replacement while the original may still be writing.

When there are no subagents, the orchestrator plays each role itself in turn, and says which role it's playing.
