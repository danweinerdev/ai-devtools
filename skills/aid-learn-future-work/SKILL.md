---
name: aid-learn-future-work
description: The Future work stage of the Learn loop, loaded by aid-learn and not an entry point. Makes sure follow-ups never float: every deferred item, follow-up finding, and "later" idea ends up in exactly one durable place, either the PRD gap, an open question, or the project's guidance.
user-invocable: false
---

# Learn, Future work: nothing floats

**Loop:** learn

Every deferred item, review finding marked follow-up, and "later" idea ends up in exactly one durable place. During Make, it's held as a state item (aid-state) until aid-learn-history resolves it. Its durable home is one of these:

- **Part of the final picture but not built yet:** that's already the PRD. The gap between the PRD and the code *is* the backlog. Nothing extra to record.
- **Not part of the final picture yet:** it's a candidate scope change, which makes it an open question for the user (aid-prd R3). Propose adding it to `## Open Questions`, or to a new PRD if it's big enough.
- **Process or tooling lessons:** the project's agent guidance or the relevant skill. Propose the edit and don't apply it silently.
