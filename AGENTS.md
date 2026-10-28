# ai-development: guide for agents working on this repository

Runtime-neutral. `CLAUDE.md` imports this file, so keep a single copy.

## What this is

A plugin of **skills** (`skills/aid-*/SKILL.md`) and **agents** (`agents/aid-*.md`) published unmodified to Claude Code, Codex, and OpenCode, plus per-harness guardrails (`harness/claude/hooks.json`, which runs `aid guard <name>` from `internal/guard/`). The design is in `docs/PRDs/AiDevelopment-PRD.md`. **Read it before changing behaviour.** It follows its own contract (`skills/aid-prd/SKILL.md`): propose edits to it as change fragments and wait for the user's approval.

## Sources vs generated

| Edit | Never hand-edit (run `make build`) |
|---|---|
| `skills/**`, `agents/*.md`, `harness/**`, `config/harness.json`, `tools/`, `scripts/`, `tests/`, `bump-version.py`, `index.js`, `tui.tsx`, `.opencode-plugin/`, docs | `skills-manifest.json`, `agents-manifest.json`, `dist/**` |

`make check` is the gate. Run it before claiming a change works. It fails on invalid frontmatter, unknown `aid-*` references, stale generated files, lint, and failing Go tests (the guards' included). Commit the regenerated files together with the source change.

## Writing rules

1. **One prose for every harness.** Skill and agent bodies are never transformed, so they must read correctly in Claude Code, Codex, and OpenCode. Don't require harness-specific tool names, slash commands, or `CLAUDE_PLUGIN_ROOT`. Refer to other skills and agents by their `aid-*` name and to bundled files by relative path.
2. **Correct without subagents.** Every skill must still work when the primary agent does all the roles itself. Delegation is an optimisation, and a serial fallback is labelled as serial.
3. **Models are Claude-native.** Agents declare `model:` (`haiku`, `sonnet`, `opus`, `fable`, or `inherit`); never `tier`. Other harnesses ignore it: OpenCode maps models in `.opencode/aid.json`, and Codex inherits.
4. **Read-only agents** set `readOnly: true` *and* list `Write, Edit` in `disallowedTools` (Claude enforcement). The build maps `readOnly` for the other harnesses.
5. **Prefix everything** with `aid-`. Codex and OpenCode share one global namespace.
6. **Skill descriptions** start with what the skill does, then when to use it, in 1024 characters or fewer. That's how every harness picks skills.
7. **Every specialist report** keeps a `## PRD changes proposed` section. It's how the PRD rule crosses the subagent boundary.
8. **No skill is a user command.** Every skill sets `user-invocable: false` and declares `**Loop:**` in its body. Stage skills are named `aid-<loop>-<stage>`, and their descriptions name the loop that loads them.
9. **The orchestrator is never an agent file.** Its rules live in the `aid-orchestrate` skill; the build generates OpenCode's primary `aid-orchestrator` from it.

## Testing the OpenCode plugin

- **Offline:** `make test-opencode` (part of `make check`, and CI) runs the plugin's Node tests against a fake OpenCode context.
- **Live:** `make test-opencode-e2e` loads this checkout into a real OpenCode 2 server and makes real model calls, so it runs locally only and never in CI. Run it only when the user asks. It proves the `aid.json` the plugin would use; see `.opencode-plugin/live/README.md`.
- **Stop code-graph first.** While a `code-graph-mcp --serve` daemon is indexing this checkout, OpenCode can't finish activating a plugin loaded from it: every load fails after 5 seconds with "Location plugins did not finish activating" (HTTP 503 from the live suite). Copies, clones, and this checkout with the daemon stopped all load immediately. Before loading the plugin from this checkout or running the live suite, stop the daemon whose working directory is this repository; code-graph starts it again on its next use. Installed copies are unaffected, because no daemon watches them.

## Versioning

Version lives in `package.json`, `.claude-plugin/plugin.json`, and `.codex-plugin/plugin.json`. Bump all three together. Harnesses cache plugins by version, so a content change without a bump is invisible to users.

`make bump-patch`, `bump-minor`, and `bump-major` run `bump-version.py`, which:

1. refuses unless the work tree is clean — no staged, partially staged or unstaged changes and no untracked files, ignored paths aside — so the sources that get tagged are exactly the ones the gate proved, and a refusal can never cost staged-only content;
2. refuses once the tag `vX.Y.Z` exists, when the three manifests disagree about the version, and when the version isn't a canonical `X.Y.Z` (no signs, spaces or leading zeros);
3. runs `make check` before writing anything, so a red tree is never bumped;
4. writes the new version to all three manifests, then runs `make build` and `make check` again, restoring the manifests if either fails;
5. commits the manifests and any regenerated artifacts as `vX.Y.Z` and creates the annotated tag `vX.Y.Z`. It never pushes.

Its tests run in throwaway git repositories (`make test-release`, part of `make check`, and Linux-only in CI); that is the only safe place to exercise the flow, because running `bump-version.py` in this checkout commits and tags. Both need `python3`.
