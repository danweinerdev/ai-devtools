---
title: Lessons carried from claude-sdd-planner
summary: What the predecessor got right, what it cost, and how each lesson shaped ai-development.
source: ../claude-sdd-planner at v2.11.0 (commit 7a74179), read 2026-10-27
---

# Lessons from claude-sdd-planner

`claude-sdd-planner` ran a full lifecycle (research → brainstorm → specify → design → plan → implement → code-review → debrief) over YAML-frontmatter artifacts, enforced by a Go binary (`sdd`) with graph plans, observation-sequence freshness, and append-only per-plan decision files. It shipped to Claude Code, plus generated Codex and OpenCode trees.

## Kept

| Lesson | Evidence in sdd-planner | Where it lives now |
|---|---|---|
| Intent-isolated review finds what plan-aware review forgives. The blind-spot lane caught defects no other lane did. | `agents/blind-spot-finder.md`; `SDD-DOGFOOD-NOTES.md` (blind-spot and fixtures lanes finding the execution-scope and isGraphPlan bugs) | `aid-make` Review lenses; `aid-oracle` |
| "Done" means verification output captured in this session. | `shared/completion-evidence.md`; devtools `executing-plans` | `aid-make` Verify; every builder's report |
| Stop on a plan-versus-reality mismatch; don't improvise. | devtools `executing-plans`; `commands/implement` Stopping Rules | `aid-make` Build; `aid-mechanic` |
| Work that's blocked on an outside answer gets refused, not built provisionally. | devtools `gated-scope` | `aid-prd` R3 |
| Evidence ranking, absence needs a search trail, premise checks. | `shared/decision-framework.md`; devtools `fact-discipline` | `aid-think` ground rules; `aid-explorer` |
| A failing contract test is never fixed by editing the assertion. | devtools `spec-fidelity` | `aid-make` Verify |
| Every piece of work traces to a demand or gets cut (`justifies`, Non-Goals). | `CLAUDE.md` ("every task carries the demand that motivates it, or it is cut") | `aid-make` Preconditions; PRD `### Non-Goals` |
| The user approves the exact statement before a durable record changes. | `AGENTS.md` (decisions written only after approval of "the exact, unmodified statement, shown in full beforehand") | `aid-prd` R1 change fragments |
| Follow-ups never float. | `shared/review-artifacts.md` | `aid-learn` Future work |
| Linear history, rebase and fast-forward, no merge commits. | `CLAUDE.md` ("never merge commits") | Make's rebuild (readable feature commits, not bisectability) |
| Committed generated trees plus a drift gate. | `make test` `TestCheckClean` | `make check` |
| Artifacts never hold credentials, hostnames, usernames, or absolute paths. | `CLAUDE.md` | `aid-prd` Frontmatter |
| Skills must work without subagents, with an honestly labelled serial fallback. | `shared/agent-runtime.md`; `shared/review-lanes.portable.md` | `aid-make` Delegate and Review |
| A schematized task graph in a Go binary: claims with leases, a kernel-released lock file, atomic temp-and-rename writes with a digest compare-and-swap, strict decoding. | `internal/store`, `internal/graph/{store,claims,model}`, `cmd/sdd/graph_stress_test.go` | The `aid` tool and `.aid/<name>/` |

## Left behind, and why

| What | Cost observed | Replacement |
|---|---|---|
| **Observation freshness and re-verification** | One-line edits reopened a verified plan from 21/21 to 11/21; stale flags were opaque; there was no "this plan shipped" notion. `VerificationFreshnessAnalysis.md` itself calls the model unconfirmed. | Each task keeps its checks, and the whole batch is re-run on demand. Nothing tracks staleness. |
| **Dolt-backed tracking (beads)** | A heavy database dependency for what is a task list. | Plain JSON files under a lock, held by `aid`. |
| **Append-only decision files** | Supersession chains, competing successors, malformed siblings acting as authority, and concurrent-append loss each needed fixes (`SDD-REFRAMING-ADVERSARIAL-REVIEW.md` R02–R13). | Decisions rewrite the PRD body. History is an optional appendix. |
| **A fixed artifact lifecycle** (research/spec/design/plan/debrief) | The overhead of five or more document types per feature. | One living PRD per feature, split when it sprawls. |
| **Prose transformation for portable trees** (`sdd plugin sync` rewrites, `*.portable.md` variants, harness markers) | Variants silently shadowed canonical edits; bare `/implement` leaked through the rewrites; OpenCode install paths contradicted each other. | Harness-neutral prose written once, `aid-` names identical everywhere. Only agent *frontmatter* is generated. |
| **Plugin-root resolution** (globbing caches, canonicalising symlinks, checking manifests) | A full section of `shared/agent-runtime.md`, repeated in every command. | Skills reference other skills by name and their own files by relative path. |
