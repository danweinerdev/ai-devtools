---
name: aid-learn-curate
description: The Curate and check stage of the Learn loop, loaded by aid-learn and not an entry point. Audits PRDs against the code and keeps the PRD set honest and scannable: inventory, drift classification, link and status hygiene, split and merge candidates, and open questions that have quietly been answered.
user-invocable: false
---

# Learn, Curate: keep the PRD set honest

**Loop:** learn

1. **Inventory.** Scan the frontmatter of every PRD under `docs/PRDs/`: status, `updated`, `code` paths, parent and children links. Delegate the scan to aid-explorer if there are many.
2. **Drift.** For each PRD in scope, check it against the code under its `code:` paths (aid-prd, "Checking a PRD against reality"): aid-explorer finds, mapping behaviour to code with evidence; you judge, or aid-oracle does when the user wants the audit independent. Classify every mismatch: the PRD follows the code, the code follows the PRD, or an open question.
3. **Hygiene.** Look for broken `parent`, `children`, or `related` links; `code:` paths that no longer exist; `status` that contradicts reality (shipped but the code is gone, draft but fully built); decision-log prose creeping into bodies; history outside the appendix.
4. **Shape.** Flag PRDs that should split (aid-prd R4) and ones that overlap enough to merge.
5. **Stale questions.** Flag `## Open Questions` that the code or a later decision has quietly answered, and propose folding the answer into the body.

PRD changes go out as fragments, one at a time. Code-follows-PRD items become Make work (aid-make).
