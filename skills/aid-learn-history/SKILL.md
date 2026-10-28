---
name: aid-learn-history
description: The History stage of the Learn loop, loaded by aid-learn and not an entry point. Closes out a piece of Make work before the rebuild drops its working state: walks every open state item with the user one at a time, resolves each to its durable home, runs the finish check, and summarises what was learned.
user-invocable: false
---

# Learn, History: close out the working state

**Loop:** learn

The rebuild drops `.aid/`. What survives it is the PRD, the project guidance, and the feature commits; the working state doesn't. So before the rebuild, everything still open in the state record gets a durable home.

1. **List what's open.** `aid items --status open` (aid-state).
2. **Resolve each item with the user, one at a time.** Give the item, its context, and your recommended destination:
   - **done:** it's already been handled; say where;
   - **a task:** it's work this feature still needs; add it to the graph and build it before finishing;
   - **the PRD:** a behaviour or layout change, or an open question; propose the fragment (aid-prd R1);
   - **project guidance:** a process or tooling lesson; propose the edit to the project's agent guidance or the relevant skill.

   Record the outcome with `aid resolve-item`.
3. **Finish.** `aid finish` must pass: every task done or dropped, no live claims, no open items. If it refuses, it lists what's left; go back to that.
4. **Summarise** for the user: what was built, what changed in the PRD, what was learned, and what's still open (now in the PRD's `## Open Questions`).

Then aid-make-rebuild rewrites the history.
