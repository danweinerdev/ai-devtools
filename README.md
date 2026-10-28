# ai-development

A judgement-first development workflow for coding agents: three optional loops, seven specialist agents, and **living PRDs** at the centre. It ships from one source to **Claude Code**, **Codex**, and **OpenCode**.

```
Think  Explore → Challenge → Clarify → Choose     decide, as PRD changes the user approves
Make   Delegate → Build → Verify → Review         build it, prove it, have it reviewed independently
Learn  History · Curate · Reference · Future work keep the PRDs true and the lessons findable
```

The full picture is in [docs/PRDs/AiDevelopment-PRD.md](docs/PRDs/AiDevelopment-PRD.md). Lessons from the predecessor are in [docs/history/sdd-planner-lessons.md](docs/history/sdd-planner-lessons.md).

## Skills

No skill is a user command. The model picks them up from what's being discussed, and the loops are modes the conversation moves through.

| Skill | Loop | Role |
|---|---|---|
| `aid-orchestrate` | all | The root's rules: the PRD gate, moving between loops, routing, briefing. |
| `aid-prd` | all | The PRD contract and its approval gate. |
| `aid-graph`, `aid-state` | all | How to drive the `aid` tool's task graph and state record. |
| `aid-think` | think | Entry point. Stages: `aid-think-explore`, `-challenge`, `-clarify`, `-choose`. |
| `aid-make` | make | Entry point. Stages: `aid-make-breakdown`, `-delegate`, `-build`, `-verify`, `-review`, `-rebuild`. |
| `aid-learn` | learn | Entry point. Stages: `aid-learn-history`, `-curate`, `-reference`, `-future-work`. |

## Agents

| Agent | Claude model | Role |
|---|---|---|
| `aid-explorer` | haiku | Read-only research with evidence |
| `aid-mechanic` | sonnet | Well-specified technical building |
| `aid-oracle` | opus | High-risk implementation and independent review |
| `aid-designer` | opus | Visual and UX direction |
| `aid-creator` | sonnet | First-pass creative work |
| `aid-craftsman` | opus | Complex refinement |

The orchestrator is never a subagent. In Claude Code it's the main conversation, which gets the `aid-orchestrate` rules at session start. In OpenCode it's the generated primary agent `aid-orchestrator`. In Codex it's the main thread, using the skill.

Models are native to each harness. Claude agents carry `model:`, and a project can override any agent's model in `.claude/aid.json`. OpenCode agents run on the session's model unless `.opencode/aid.json` maps them to a profile. Codex agents inherit the session model.

## Guardrails (Claude Code)

`harness/claude/hooks.json` wires the hooks into Claude Code; each runs `aid guard <name>`, so **`aid` must be on your PATH** (see below). If it isn't, the hooks error and Claude Code carries on without them. They block generic delegation (`general-purpose`, `Explore`, `Plan`), cap inline exploration with a pointer to `aid-explorer`, apply per-project model overrides, and optionally gate sensitive content. Their settings live in `.claude/aid.json` (`models`, `guards`, `sensitive`). The file is validated strictly: an invalid one is reported at session start, and the guards deny with what to fix until it is. Set `AID_GUARDS=off` to disable them for a session. Sensitivity `patterns` are RE2 regular expressions (no lookaround or backreferences).

## Install

**Claude Code** loads the repo root as a plugin (`skills/` and `agents/` are discovered):

```bash
claude --plugin-dir /path/to/ai-development
```

**Codex** reads `skills/` from `.codex-plugin/plugin.json`. Codex plugins can't declare agents, so link those separately:

```bash
make install-codex        # symlinks dist/codex/agents/*.toml into $CODEX_HOME/agents
```

**OpenCode 2** (2.x only) loads the repo directory as a plugin, which registers the `aid-*` agents (`aid-orchestrator` as the default primary agent) and skills itself, routes each agent's model from `.opencode/aid.json` through `aid`, and sends every tool call to `aid guard`. Nothing to symlink; `aid` must be on your PATH:

```jsonc
// opencode.json
{ "plugins": ["/path/to/ai-development"] }
```

See [.opencode-plugin/README.md](.opencode-plugin/README.md) for routing, guards and the sidebar. `make test-opencode` runs its offline tests (part of `make check` where node is installed); `make test-opencode-e2e` runs the live suite against real models, locally only ([.opencode-plugin/live/README.md](.opencode-plugin/live/README.md)).

## The `aid` tool

Make work keeps its task graph and state record in `.aid/<name>/`, held by `aid`, a Go binary built from this repo. Every command validates both files, writes are locked and atomic, and it never calls git. It needs Go 1.26.10; `make` fetches that toolchain when the installed Go is older.

```bash
make install-aid    # go install ./cmd/aid, into GOBIN or GOPATH/bin
# or, without a checkout:
# go install github.com/danweinerdev/ai-devtools/cmd/aid@latest
```

A short walkthrough, from the repository root:

```bash
aid init AiDevelopment                        # .aid/AiDevelopment/, named after the PRD
aid add-task --id store --title "The store" --footprint internal/store/ \
  --result "round-trip tests pass" --check "go test ./internal/store"
aid ready                                     # the frontier: todo tasks whose needs are done
aid claim-task store                          # in-progress, held by $AID_HOLDER or "main", 2h lease
aid complete-task store                       # runs the checks; done only if they pass
                                              # (a delegated worker adds --holder <its role>)
aid add-item --kind follow-up --text "..."    # anything that isn't a task
aid check                                     # every task's checks, as one batch
aid finish                                    # succeeds only when nothing is left
```

Add `--json` to any command for machine-readable output. The skills `aid-graph` and `aid-state` are the full guides.

## Develop

```bash
make build   # regenerate the manifests and the per-harness agents
make check   # the gate: validation, cross-references, generated files up to date, lint, Go tests (guards included)
```

Edit `skills/`, `agents/`, and `harness/`. The manifests and `dist/` are generated by `tools/plugin-compiler` (Go; needs a Go toolchain, no Node). See [AGENTS.md](AGENTS.md).
