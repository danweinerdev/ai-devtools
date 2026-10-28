---
name: aid-make-delegate
description: The Delegate stage of the Make loop, loaded by aid-make and not an entry point. Picks ready tasks from the task graph, claims them, and routes each to the cheapest aid-* role or project agent that can succeed, with a complete brief. Independent tasks go out in parallel.
user-invocable: false
---

# Make, Delegate: claim, route, brief

**Loop:** make

1. **Pick.** Take tasks from the ready frontier (`aid ready`). Tasks whose footprints don't overlap can run in parallel; overlapping ones run in sequence.
2. **Claim.** `aid claim-task <id> --holder <role>`, so no two builders take the same task (aid-graph).
3. **Route** to the cheapest role that can succeed:

| Role | Use for | Not for |
|---|---|---|
| aid-explorer | research, locating, comparing options, reading branches and references | deciding |
| aid-mechanic | well-specified technical building: features from a clear spec, tests, wiring, refactors | open design questions |
| aid-oracle | high-risk or novel implementation (concurrency, security, data migration, core algorithms) | routine work, where it's wasted |
| aid-designer | visual and UX direction: flows, layout, interaction, visual language | writing production code |
| aid-creator | first-pass creative work: drafts, copy, prototypes, sketches, first versions | final polish |
| aid-craftsman | complex refinement: turning a working first pass into a finished one, hard polish, deep cleanup | greenfield first passes |

Project agents are routing targets too, when their description fits the task better than a built-in role. Models are configured per harness; don't name models in briefs.

## The brief

A subagent knows only its brief. Include:

- the goal, and the task's ID;
- the governing PRD sections, by path and heading;
- the footprint: the files in scope;
- the definition of done and the exact check commands that must pass;
- what's already been ruled out;
- the report shape, including `## PRD changes proposed` and anything noticed that isn't part of the task.

When there are no subagents, the orchestrator plays each role itself in turn, and says which role it's playing.
