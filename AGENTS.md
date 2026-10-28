# ai-development: guide for agents working on this repository

Runtime-neutral. `CLAUDE.md` imports this file, so keep a single copy.

## What this is

A plugin of **skills** (`skills/aid-*/SKILL.md`) and **agents** (`agents/aid-*.md`) published unmodified to Claude Code, Codex, and OpenCode, plus per-harness guardrails (`harness/claude/hooks.json`, which runs `aid guard <name>` from `internal/guard/`). The design is in `docs/PRDs/AiDevelopment-PRD.md`. **Read it before changing behaviour.** It follows its own contract (`skills/aid-prd/SKILL.md`): propose edits to it as change fragments and wait for the user's approval.

## Sources vs generated

| Edit | Never hand-edit (run `make build`) |
|---|---|
| `skills/**`, `agents/*.md`, `harness/**`, `config/harness.json`, `tools/`, `scripts/`, `index.js`, docs | `skills-manifest.json`, `agents-manifest.json`, `dist/**` |

`make check` is the gate. Run it before claiming a change works. It fails on invalid frontmatter, unknown `aid-*` references, stale generated files, lint, and failing Go tests (the guards' included). Commit the regenerated files together with the source change.

## Writing rules

1. **One prose for every harness.** Skill and agent bodies are never transformed, so they must read correctly in Claude Code, Codex, and OpenCode. Don't require harness-specific tool names, slash commands, or `CLAUDE_PLUGIN_ROOT`. Refer to other skills and agents by their `aid-*` name and to bundled files by relative path.
2. **Correct without subagents.** Every skill must still work when the primary agent does all the roles itself. Delegation is an optimisation, and a serial fallback is labelled as serial.
3. **Models are Claude-native.** Agents declare `model:` (`haiku`, `sonnet`, `opus`, `fable`, or `inherit`); never `tier`. Other harnesses ignore it: OpenCode maps models in its router config, and Codex inherits.
4. **Read-only agents** set `readOnly: true` *and* list `Write, Edit` in `disallowedTools` (Claude enforcement). The build maps `readOnly` for the other harnesses.
5. **Prefix everything** with `aid-`. Codex and OpenCode share one global namespace.
6. **Skill descriptions** start with what the skill does, then when to use it, in 1024 characters or fewer. That's how every harness picks skills.
7. **Every specialist report** keeps a `## PRD changes proposed` section. It's how the PRD rule crosses the subagent boundary.
8. **No skill is a user command.** Every skill sets `user-invocable: false` and declares `**Loop:**` in its body. Stage skills are named `aid-<loop>-<stage>`, and their descriptions name the loop that loads them.
9. **The orchestrator is never an agent file.** Its rules live in the `aid-orchestrate` skill; the build generates OpenCode's primary `aid-orchestrator` from it.

## Versioning

Version lives in `package.json`, `.claude-plugin/plugin.json`, and `.codex-plugin/plugin.json`. Bump all three together. Harnesses cache plugins by version, so a content change without a bump is invisible to users.
