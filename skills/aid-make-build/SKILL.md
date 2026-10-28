---
name: aid-make-build
description: The Build stage of the Make loop, loaded by aid-make and not an entry point. The builder works inside the brief's scope and footprint, stops and reports on any mismatch between the brief, the PRD, and reality instead of improvising, and proposes PRD fragments for inconsistencies it finds.
user-invocable: false
---

# Make, Build: inside the brief

**Loop:** make

- **Stay inside the footprint.** A change outside the files in scope is either a sign the plan is wrong (report it) or scope creep (don't).
- **Stop on any mismatch.** If reality doesn't match the brief, the PRD, or the graph (a file isn't where it should be, an API behaves differently, a step can't work as written), stop and report it. Don't improvise around it. The orchestrator decides: update the task, split it, drop it, or take it to aid-think.
- **Inconsistencies become fragments.** A builder that finds a PRD inconsistency writes the fragment itself (aid-prd R2) in its report under `## PRD changes proposed`. The orchestrator shows it to the user.
- **Report what isn't the task.** Notes, follow-ups, and questions noticed along the way go in the report. The orchestrator records them as state items (aid-state).
- **Commit freely while iterating.** History gets rebuilt at the end (aid-make-rebuild).

When the build ends, hand to aid-make-verify. If the plan has to change, the orchestrator changes the graph (aid-graph): split, update, or drop tasks, never silently work around them.
