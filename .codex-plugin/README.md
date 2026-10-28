# Codex

`plugin.json` here is the Codex manifest, and `"skills": "./skills/"` points at the shared skill tree at the repo root.

Codex plugins can't declare agents (the manifest supports skills, MCP servers, apps, and hooks). Agent roles install as files instead: `make install-codex` symlinks `dist/codex/agents/*.toml` into `$CODEX_HOME/agents/`, where Codex discovers roles that its spawn tool can select by name.
