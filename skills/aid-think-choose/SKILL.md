---
name: aid-think-choose
description: The Choose stage of the Think loop, loaded by aid-think and not an entry point. Turns the user's answers into durable truth: restates each decision, checks it against the open conflicts, and writes it into the PRD as a change fragment, with anything left open recorded under Open Questions.
user-invocable: false
---

# Think, Choose: the user decides

**Loop:** think

Goal: turn answers into durable truth.

- Restate each decision in one line and check it against the conflicts list. If it resolves one conflict but opens another, say so.
- Turn decisions into **PRD change fragments** (aid-prd R1), one at a time. Rewrite the affected sections so the PRD reads as the final picture, not as "we decided X".
- Things the user chose to leave open go into `## Open Questions`, with your recommendation.
- If the scope has outgrown one PRD, propose a split (aid-prd R4).
- If no PRD exists and the user wants one, draft it from [aid-prd's template](../aid-prd/references/template.md) and present it whole for approval.

Then either loop (the answer raised new questions, so go back to aid-think-explore or aid-think-clarify) or hand off.
