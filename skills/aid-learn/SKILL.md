---
name: aid-learn
description: The Learn loop, covering work history, curate and check, reference, and future work. Use it when a piece of Make work is finishing and its state needs closing out; when asked to audit or curate PRDs against the code or find stale or oversized PRDs; to record what was learned after a feature or session; to capture reference material for later; or to decide where follow-ups and future work should live.
user-invocable: false
---

# Learn: history, curate, reference, future work

**Loop:** learn

Make produces changes. Learn makes sure the knowledge behind them survives: the PRDs stay true, the working state is closed out before it's dropped, references stay findable, and future work doesn't float.

## Stages

| Stage | Skill | When | Works from |
|---|---|---|---|
| History | aid-learn-history | a piece of Make work is finishing: resolve its state items before the rebuild drops them | the work's `.aid/` state record |
| Curate and check | aid-learn-curate | an audit of PRDs against the code, or PRD hygiene | the PRDs and the code under their `code:` paths |
| Reference | aid-learn-reference | material worth keeping that isn't a PRD | the material and its source |
| Future work | aid-learn-future-work | deciding where a deferred item or "later" idea lives | the item |

These stages are a toolbox, not a map. Pick the one the situation calls for, skip or revisit any of them, and when the next step isn't obvious, suggest one or ask the user what they want to do. "Works from" says what each stage needs: skipping the stage that produces it is a choice to make knowingly, and to say out loud. Everything that changes a PRD goes out as fragments for approval (aid-prd R1), one at a time. Work that's part of the final picture is already tracked by the gap between the PRD and the code. Work that isn't is an open question.
