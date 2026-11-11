# Every criterion maps to a check

The proof that the feature works is not the tests that were written; it is that every acceptance criterion in the PRD sections being built has at least one task check that exercises it. The breakdown makes that mapping visible while the plan is still text:

| Acceptance criterion | Task | Check |
|---|---|---|
| PRD › Make › "a task is `done` only when its checks pass" | `store` | `go test ./internal/store -run '^TestDoneRequiresPassingChecks$'` |
| PRD › Make › "a check gets ten minutes" | `checks` | `go test ./cmd/aid -run '^TestCheckTimeout$'` |

Rules:

- **List the mapping in the plan,** next to each step. A criterion may map to more than one check; every criterion needs at least one.
- **The check must exercise the criterion,** not merely touch the file. A test of an unrelated path, or a whole-suite command kept only for reassurance, doesn't satisfy the mapping.
- **No check means an open state item, recorded now.** Use `aid add-item --kind follow-up --text "<criterion> has no check: <what is missing>"` when the work still has to add the check, or `--kind question` when only the user can say what would prove the criterion (no test seam, the evidence is ambiguous, the proof is manual). aid-state has the kind definitions. Either way the item is open, so `aid finish` refuses while the criterion is unproven.
- **Resolve it with a check, not a promise.** Close the item only once the check exists and belongs to a task; `--as task --where <id>` or `--as done --where <where the check lives>`.
- **The graph stays authoritative.** Once the breakdown is approved and the graph built, changes to a task's checks go through `update-task` (aid-graph); the mapping is a plan-time artifact and the text isn't kept in sync.
