#!/usr/bin/env sh
# Installs the generated agent files where each harness discovers them.
# Skills need no install step on any harness:
#   Claude Code  loads the plugin root (skills/, agents/) via .claude-plugin/
#   Codex        loads skills/ via .codex-plugin/plugin.json; agents are not
#                a plugin resource, so they are linked into $CODEX_HOME/agents
#   OpenCode     registers skills via index.js; agents are linked into
#                ~/.config/opencode/agents
#
# Usage: scripts/install.sh codex|opencode [--uninstall]
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
harness=${1:?usage: install.sh codex|opencode [--uninstall]}
mode=${2:-}

case "$harness" in
  codex)    src="$ROOT/dist/codex/agents";    dest="${CODEX_HOME:-$HOME/.codex}/agents" ;;
  opencode) src="$ROOT/dist/opencode/agents"; dest="${XDG_CONFIG_HOME:-$HOME/.config}/opencode/agents" ;;
  *) echo "unknown harness: $harness" >&2; exit 2 ;;
esac

mkdir -p "$dest"
for f in "$src"/aid-*; do
  target="$dest/$(basename "$f")"
  if [ "$mode" = "--uninstall" ]; then
    [ -L "$target" ] && rm "$target" && echo "removed $target"
    continue
  fi
  if [ -e "$target" ] && [ ! -L "$target" ]; then
    echo "skip $target: exists and is not our symlink" >&2
    continue
  fi
  ln -sfn "$f" "$target" && echo "linked $target"
done
