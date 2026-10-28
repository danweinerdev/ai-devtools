---
name: aid-think-clarify
description: The Clarify stage of the Think loop, loaded by aid-think and not an entry point. Gets the decisions only the user can make by asking one question at a time, each with context, evidence, options, and a recommendation, and waiting for the answer before the next.
user-invocable: false
---

# Think, Clarify: ask the user

**Loop:** think

Goal: get the decisions only the user can make.

- Ask **one question at a time**, starting with the one that blocks the most. The next question waits for the answer.
- Give the question its context (one or two lines plus the evidence), the options, and your **recommendation** with its reason. A question without a recommendation is allowed only when you truly have no basis to choose.
- When the answer changes other queued questions, say so, and re-rank the queue.
- Expect follow-up questions. Answers raise new questions, and that's the loop working.
- Use the harness's structured question tool when it has one. Otherwise ask in prose with the options listed.

Each answer goes to aid-think-choose.
