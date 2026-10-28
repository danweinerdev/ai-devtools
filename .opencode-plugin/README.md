# OpenCode 2

The OpenCode plugin: OpenCode 2 only. It has no manifest file; OpenCode loads the package root, resolving the entry from `package.json` `exports` (`.` is `./index.js`) or a root `index.js`, and the TUI half from the root `tui.tsx`.

## Install

List the repository directory under `plugins` in `opencode.json` (project or `~/.config/opencode/opencode.json`):

```jsonc
{ "plugins": ["/path/to/ai-development"] }
```

`file:///path/to/ai-development` works too. Point it at the **directory**: OpenCode 2.2.0 refuses a file path (`configured plugin path must be a directory`). A fresh clone loads in about a second. A checkout that a code-graph daemon (`code-graph-mcp --serve`) is indexing can't be loaded: OpenCode never finishes activating it and fails after five seconds with `Location plugins did not finish activating`. Stop that daemon first (code-graph restarts it on next use), or load the plugin from a clone no daemon watches.

`aid` must be on your PATH (`make install-aid`): model routing and the guards run through it. Without it the plugin still registers everything, warns, runs every agent on the session's model, and lets every tool call through.

## What it does

All from the plugin's `setup`, with nothing copied or symlinked:

- **Agents.** The six `aid-*` specialists as subagents and `aid-orchestrator` as the default primary agent, through `ctx.agent.transform` (its `update` creates an agent that doesn't exist). Descriptions, modes and prompts come from the build's `dist/opencode/agents/*.md`; a read-only agent gets the rule `{action: "edit", resource: "*", effect: "deny"}`.
- **Skills.** Every `aid-*` skill from `skills-manifest.json`, through `ctx.skill.transform`, pointing back into this package.
- **Models.** `aid config --harness opencode --json` resolves `.opencode/aid.json` (project) or `~/.config/opencode/aid.json` (global); aid validates it, and an invalid one is reported loudly and to the orchestrator. Each agent the file maps to a profile runs on that profile's model (`provider/model#variant`, with `reasoning_effort`); an unmapped agent, or one mapped to the reserved `"inherit"`, uses the session's model, so a config can list every agent before you choose its profile. A routed `aid-orchestrator` also becomes the default model, because OpenCode runs primary turns on the default model, not the agent's own. A variant the model doesn't offer is dropped with a warning. `hide_free_models` hides OpenCode's free models.
- **Guards.** Every tool call goes to `aid guard opencode-tool` first; a deny blocks the call with aid's reason. aid missing, failing or timing out lets the call through (reported once). The orchestrator's routing policy comes from `aid guard policy --harness opencode`.
- **Sidebar.** An "Active sub-agents" panel in the TUI sidebar lists the conversation's running sub-agents and the model each is on.

## Layout

| Path | What |
|---|---|
| `../index.js` | the entry: default export `{id, setup}` only |
| `../tui.tsx` | re-exports `tui.tsx` for OpenCode's `<dir>/tui` lookup |
| `src/setup.js` | the adapter: registration, routing, guard bridge, hooks |
| `src/aid.js` | runs `aid` (no shell, with a timeout); never throws |
| `src/agents.js`, `src/skills.js` | read the generated agents and the skills manifest |
| `src/routing.js` | profiles to model refs, variant and free-model checks |
| `src/active-subagents.js`, `tui.tsx` | the sidebar |
| `test/*.test.js` | offline tests: `make test-opencode` (part of `make check` where node exists) |
| `live/` | the live suite (kept out of `test/` so a bare `node --test` never runs it): `make test-opencode-e2e` (real model calls; local only) |

Ported from opencode-model-router's OpenCode 2 adapter.
