---
name: aid-think-challenge
description: The Challenge stage of the Think loop, loaded by aid-think and not an entry point. Lines up the user's ideas, the PRD, references, and code, and states every conflict, contradiction with the facts, gap, piece of hidden scope, and risk directly, ranked by what blocks the most downstream work. Also runs the adversarial PRD challenge, through isolated coherence, goals, and grounding lenses, when the user asks for one while deciding whether a PRD is ready to build.
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

## The adversarial PRD challenge

Run it only when the user asks: they're deciding whether a PRD is ready to build and want it attacked first. It gives no verdict. Readiness is the user's call, made with everything else they know; your job is to find what would hurt.

Each lens gets only its inputs and runs in its own context, as aid-oracle, so no lens can forgive what another would catch. Dispatch them in parallel, and don't pre-read their inputs into your own context.

| Lens | Gets | Attacks |
|---|---|---|
| Coherence | the PRD alone | contradictions between sections; ambiguity two builders would resolve differently; statements no check could prove; behaviour described without its failure case; terms used but never defined |
| Goals | the overview and goals, plus the behaviour sections | behaviour that serves no stated goal; goals that nothing described achieves |
| Grounding | the PRD, the code under its `code:` paths, and related PRDs, with the code evidence gathered first by aid-explorer | assumptions about existing code or sibling PRDs that don't hold, each with file:line |

Merge the lens reports into one list ranked by what would hurt most if built as written, each challenge with its lens and evidence. Challenges two lenses raise are strong. Present the list, then take the challenges the user wants to pursue through aid-think-clarify and aid-think-choose: each one they accept becomes a change fragment or an open question (aid-prd). Rejected ones are dropped without argument.

When the lenses can't run in isolated contexts, run them one after another and **label the challenge as serial**.
