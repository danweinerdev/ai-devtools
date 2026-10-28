#!/usr/bin/env sh
# Installs the generated Codex agent files where Codex discovers them. No other
# harness needs an install step:
#   Claude Code  loads the plugin root (skills/, agents/) via .claude-plugin/
#   Codex        loads skills/ via .codex-plugin/plugin.json; agents are not
#                a plugin resource, so they are linked into $CODEX_HOME/agents
#   OpenCode     the plugin (index.js) registers agents and skills itself
#
# Usage: scripts/install.sh codex [--uninstall]
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
harness=${1:?usage: install.sh codex [--uninstall]}
mode=${2:-}

case "$harness" in
  codex)    src="$ROOT/dist/codex/agents";    dest="${CODEX_HOME:-$HOME/.codex}/agents" ;;
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
