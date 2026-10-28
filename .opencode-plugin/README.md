# OpenCode

OpenCode has no plugin manifest file. It loads a plugin as a package: its `package.json` `main` (`../index.js`), listed in `opencode.json`:

```jsonc
{ "plugins": ["file:///path/to/ai-development"] }
```

- **Skills:** `index.js` registers every skill from `skills-manifest.json` in place, through the V2 `ctx.skill.transform` API.
- **Agents:** the V2 agent editor can update and remove agents but can't add them, so agents install as files. `make install-opencode` symlinks `dist/opencode/agents/*.md` into `~/.config/opencode/agents/`.
- **Models:** OpenCode ignores the Claude `model:` key. Map each `aid-*` agent to a model slot in the model-router plugin's `agents` map.
- **Orchestrator:** `aid-orchestrator` is a primary agent generated from the `aid-orchestrate` skill. Run it as the root.

This directory holds documentation only. Everything OpenCode loads is at the repo root or in `dist/opencode/`.
