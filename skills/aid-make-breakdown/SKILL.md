---
name: aid-make-breakdown
description: The Breakdown stage of the Make loop, loaded by aid-make and not an entry point. Maps the path from the current code to what the PRD describes, in three steps: the gap, a strategy the user chooses, and a plan whose every step ends in a measurable result. The user reviews it as text; the orchestrator then builds the task graph from it, task by task.
user-invocable: false
---

# Make, Breakdown: gap, strategy, plan

**Loop:** make

The breakdown is the path from where the code is now to what the PRD describes. The PRD is the lens. The breakdown never changes what the feature does, only how we get there.

## 1. Gap

Measure the code against the PRD: what exists, what's missing, and what's wrong. Cite PRD sections and file paths. Delegate wide reads to aid-explorer. Anything gated by an open question is marked gated, never planned around a guessed answer (aid-prd R3).

## 2. Strategy

Recommend one with reasons; the user chooses.

- **Map-reduce:** every part is built and tested in isolation, in sequence or in parallel, then an assembly stage puts them together. Fits when the parts are largely independent.
- **Foundation first, then piece by piece:** a minimum path through all the requirements that achieves one real goal end to end. Most of it is shallow or hard-coded, with a few deliberately real pieces. Features are then built on top one by one. Existing code is a foundation already in place.

The catalog grows. If neither fits, say so and describe the shape you'd use.

## 3. Plan

In the chosen strategy's shape. Each step is a task with:

- **What it builds,** citing the PRD section it serves.
- **Needs:** the tasks it depends on.
- **Footprint:** the files, and where useful the classes or functions, it creates or edits.
- **Definition of done:** the measurable result, and the **checks** that prove it, usually the tests written for the task. Checks are commands that run non-interactively from the repository root and pass or fail by exit code.
- **Role:** the aid-* role that should build it (aid-make-delegate).

A step with no observable result isn't a step. New seams and component responsibilities, and for new systems the layout itself, go to the PRD's `## Layout` as fragments (aid-prd R1).

## Review, then build the graph

1. Write the breakdown to `.aid/<name>/breakdown.md`, with `<name>` taken from the PRD it serves, and present it to the user as prose.
2. Apply the user's edits until they approve it.
3. Build the task graph from it task by task: `aid init <name>`, then one `aid add-task` per step (aid-graph). The graph is authoritative from here. The breakdown text is the first map and isn't updated as the plan changes; the graph is.
