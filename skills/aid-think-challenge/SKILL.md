---
name: aid-think-challenge
description: The Challenge stage of the Think loop, loaded by aid-think and not an entry point. Lines up the user's ideas, the PRD, references, and code, and states every conflict, contradiction with the facts, gap, piece of hidden scope, and risk directly, ranked by what blocks the most downstream work.
user-invocable: false
---

# Think, Challenge: test ideas against facts

**Loop:** think

Goal: surface every place where ideas, documents, and code disagree before anyone decides.

Line up the user's stated ideas, the PRD, the references, and the code from the fact sheet, and list:

- **Conflicts:** two sources say different things. Quote both.
- **Contradictions with facts:** an idea or answer that the evidence says won't work as stated, and why.
- **Gaps:** behaviour the PRD needs but doesn't define.
- **Hidden scope:** an idea that quietly expands a component's responsibility.
- **Risk:** where the obvious choice fails (concurrency, migration, failure modes, performance, security, cost).

Be direct. Rank by what blocks the most downstream work.

For a high-risk technical judgement, get a second opinion from aid-oracle. For UX or visual direction, use aid-designer. Pass them the question and the evidence, not your conclusion.

The ranked list becomes the question queue for aid-think-clarify.
