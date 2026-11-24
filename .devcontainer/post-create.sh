#!/bin/sh
# Runs once per container build (postCreateCommand). Safe to re-run: the
# Claude config and the gh login live in named volumes that survive
# rebuilds, so plugins, marketplaces and credentials may already be there.
set -eu

claude_dir="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"

# A fresh named volume is mounted root-owned.
sudo chown -R "$(id -u):$(id -g)" "$claude_dir" "$HOME/.config/gh" \
	"$HOME/.config/atlas" "$HOME/.local/share/atlas"
# Docker creates missing parents of a mount point as root.
sudo chown "$(id -u):$(id -g)" "$HOME/.config" "$HOME/.local" "$HOME/.local/share"

# One-time restore of a pre-volume snapshot (see save-claude.sh).
snapshot="$(dirname "$0")/.claude-snapshot.tgz"
if [ -f "$snapshot" ]; then
	if [ ! -e "$claude_dir/.claude.json" ]; then
		tar -xzf "$snapshot" -C "$claude_dir"
		echo "Restored Claude history from $snapshot"
	fi
	rm -f "$snapshot"
fi

# One-time restore of a pre-volume atlas snapshot (see save-atlas.sh). The
# notes go to ~/sshctl-notes, a folder on the host mounted by devcontainer.json.
atlas_snapshot="$(dirname "$0")/.atlas-snapshot.tgz"
if [ -f "$atlas_snapshot" ]; then
	tmp="$(mktemp -d)"
	tar -xzf "$atlas_snapshot" -C "$tmp"
	[ -e "$HOME/.config/atlas/atlas.json" ] || cp -a "$tmp/config/." "$HOME/.config/atlas/"
	[ -e "$HOME/.local/share/atlas/state/memory.sqlite" ] ||
		{ mkdir -p "$HOME/.local/share/atlas/state" && cp -a "$tmp/data/state/." "$HOME/.local/share/atlas/state/"; }
	cp -an "$tmp/notes/." "$HOME/sshctl-notes/"
	rm -rf "$tmp" "$atlas_snapshot"
	echo "Restored atlas memory and notes from $atlas_snapshot"
fi

npm install --global @openai/codex@latest @anthropic-ai/claude-code@latest

codex plugin marketplace add mattpocock/skills || true
codex plugin add mattpocock-skills@mattpocock || true
claude plugin marketplace add mattpocock/skills || true
claude plugin install mattpocock-skills@mattpocock || true
