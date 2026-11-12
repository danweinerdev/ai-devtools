# Breakdown: dedicated reviewers, review gates, `effort`

Governing PRD: `docs/PRDs/AiDevelopment-PRD.md` (sections: The agent roster; Make › Review gates; Make › When to verify and review; Think › adversarial challenge; Agent frontmatter; Models are native to each harness).

## 1. Gap

| PRD says | Code today |
|---|---|
| Four read-only reviewers (auditor `opus`/high, navigator `opus`/high, inspector `haiku`/high, skeptic `sonnet`/high) | Absent; `agents/` has 7 agents |
| aid-oracle builds, never reviews | `agents/aid-oracle.md` description, `capabilities` (`isolated-review`, `prd-challenge`), `## Reviewing` section and report's `review:`/`PRD challenge:` lines |
| Lens → reviewer staffing; risky-task inspector; skips reported | `skills/aid-make-review/SKILL.md:38` "aid-oracle is the default reviewer"; timing rule 3 still "high-risk work earns a review" |
| Bug-class checklist in aid-inspector's prompt | `skills/aid-make-review/references/bug-classes.md` (64 lines) |
| Challenge lenses: coherence→skeptic, goals→navigator, grounding→auditor | `skills/aid-think-challenge/SKILL.md:31` "as aid-oracle" |
| Independent PRD-vs-code judgement by aid-auditor | `skills/aid-orchestrate/SKILL.md:47,53`, `skills/aid-prd/SKILL.md:120`, `skills/aid-learn-curate/SKILL.md:12` name aid-oracle |
| Every feature ends in a review-gate task | `skills/aid-make-breakdown/SKILL.md` Plan, `skills/aid-graph/SKILL.md` "Building the graph" say nothing |
| `effort` validated by the build | `tools/plugin-compiler/compile.go` ignores it |
| Routing names the reviewers | `internal/guard/opencode.go:35` roster, `session.go:26,35,49,58`, `guard.go:146` ("deep review: aid-oracle"); `skills/aid-make-delegate/SKILL.md` table |
| OpenCode registers/routes every agent | `.opencode-plugin/live/e2e.js:40` SUBAGENTS, `test/plugin.test.js:10` ROSTER, `test/e2e-plan.test.js:20` skipped list |
| Docs | `README.md:32` agent table; `AGENTS.md` rule 3 (models) doesn't mention `effort` |

No open question gates this work.

## 2. Strategy

**Map-reduce.** The parts are independent edits (agent prompts, compiler, Go guards, skill prose, OpenCode tests), then an assembly stage regenerates and runs the gate. Authoring tasks that touch `agents/` or `skills/` share the generated files (`*-manifest.json`, `dist/`), so they run in sequence; the Go and compiler tasks run in parallel with them.

## 3. Plan

| ID | Builds (PRD section) | Needs | Footprint | Role |
|---|---|---|---|---|
| `reviewer-agents` | The four reviewer agents, adapted from claude-sdd-planner's `spec-compliance`, `drift-detector`, `quality-scanner`, `blind-spot-finder`, each holding its Make lens and its Think challenge lens; the bug-class checklist moved into aid-inspector; aid-oracle stripped to building (roster) | — | `agents/`, `agents-manifest.json`, `dist/` | aid-craftsman (refining a working first pass: the sdd prompts) |
| `compiler-effort` | `effort` validated as `low\|medium\|high\|xhigh\|max` (Agent frontmatter) | — | `tools/plugin-compiler/` | aid-mechanic |
| `guard-roster` | Guard roster and routing policy name the reviewers; aid-oracle no longer "deep review" (roster; Guardrails › Routing policy) | — | `internal/guard/`, `cmd/aid/` | aid-mechanic |
| `skills-staffing` | aid-make-review (staffing, risky-task inspector, reported skips, checklist reference removed), aid-think-challenge, aid-orchestrate, aid-make-delegate, aid-prd, aid-learn-curate; review-gate rule in aid-make-breakdown and aid-graph; README table; AGENTS.md rule 3 | `reviewer-agents` | `skills/`, `skills-manifest.json`, `dist/`, `README.md`, `AGENTS.md` | aid-mechanic |
| `opencode-roster` | OpenCode tests and live suite know the reviewers | `reviewer-agents` | `.opencode-plugin/` | aid-mechanic |
| `assembly` | Regenerated artifacts; whole gate green | all above | `agents-manifest.json`, `skills-manifest.json`, `dist/` | orchestrator |
| `review-gate` | Review lenses ran and every finding has an outcome (Make › Review gates); also the PR gate | `assembly` | union of the above | orchestrator |

### Definitions of done

- **reviewer-agents**: four agent files with the PRD's model and effort, read-only, each with a `## PRD changes proposed` report section; oracle carries no review role.
  - `go run ./tools/plugin-compiler --check`
  - `for a in auditor:opus navigator:opus inspector:haiku skeptic:sonnet; do f=agents/aid-${a%%:*}.md; grep -q "^model: ${a#*:}$" $f && grep -q '^effort: high$' $f && grep -q '^readOnly: true$' $f && grep -q '^## PRD changes proposed' $f || exit 1; done`
  - `! grep -nE 'isolated-review|prd-challenge|^## Reviewing|independent review' agents/aid-oracle.md`
  - `grep -q 'Test quality' agents/aid-inspector.md`
- **compiler-effort**: a bad `effort` fails the build, a good one and an absent one pass. Red first.
  - `go test ./tools/plugin-compiler -run Effort`
- **guard-roster**: roster contains the reviewers; both session policies route review to them and not to aid-oracle.
  - `go test ./internal/guard/... ./cmd/aid/...`
- **skills-staffing**:
  - `go run ./tools/plugin-compiler --check`
  - `! grep -rnE 'default reviewer|as aid-oracle|or aid-oracle in its own context|aid-oracle does when' skills`
  - `test ! -e skills/aid-make-review/references/bug-classes.md`
  - `grep -q 'review-gate' skills/aid-make-breakdown/SKILL.md && grep -q 'review-gate' skills/aid-graph/SKILL.md`
  - `grep -q 'aid-inspector' skills/aid-make-review/SKILL.md && grep -q 'aid-auditor' skills/aid-think-challenge/SKILL.md`
- **opencode-roster**: `make test-opencode`
- **assembly**: `make check`
- **review-gate**: result "the review lenses ran and every finding has an outcome"; checks: every task's checks above.

### Criterion → check

| PRD criterion | Check |
|---|---|
| Four reviewers with model/effort, read-only | reviewer-agents #2 |
| Oracle builds; never reviews | reviewer-agents #3; guard-roster test |
| Bug-class checklist in aid-inspector | reviewer-agents #4; skills-staffing #3 |
| Lens → reviewer staffing (Make and challenge) | skills-staffing #2, #5 |
| Independent PRD-vs-code judgement by aid-auditor | skills-staffing #2 |
| Review-gate tasks in the plan | skills-staffing #4 |
| Risky-task inspector; reported skips | **no mechanical check**: prose, judged at review-gate (state item) |
| `effort` validated | compiler-effort |
| Each reviewer routable in OpenCode | opencode-roster |
| Routing policy names reviewers | guard-roster |

### Notes

- **The gate's reviewers.** The installed plugin (v0.2.0) has no reviewer agents, so at `review-gate` they can't be dispatched unless the checkout is loaded (or a version is installed). Question for the user before the gate.
- No version bump in this plan; `make bump-*` only when asked.
