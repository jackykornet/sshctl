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

# vhs records the Workspace GIFs (docs/demo/*.tape). Its release binary,
# since building it needs a newer Go than the repo pins; it drives ttyd,
# ffmpeg and Chromium.
vhs_version=0.12.1
ttyd_version=1.7.7
if ! vhs --version 2>/dev/null | grep -q "v$vhs_version"; then
	case "$(uname -m)" in
	x86_64) vhs_arch=x86_64 ttyd_arch=x86_64 ;;
	aarch64 | arm64) vhs_arch=arm64 ttyd_arch=aarch64 ;;
	*) echo "vhs: no release for $(uname -m)" >&2 && exit 1 ;;
	esac
	sudo apt-get update -qq
	sudo apt-get install -y -qq ffmpeg chromium
	tmp="$(mktemp -d)"
	curl -fsSL -o "$tmp/ttyd" "https://github.com/tsl0922/ttyd/releases/download/$ttyd_version/ttyd.$ttyd_arch"
	curl -fsSL "https://github.com/charmbracelet/vhs/releases/download/v$vhs_version/vhs_${vhs_version}_Linux_$vhs_arch.tar.gz" |
		tar -xz -C "$tmp"
	sudo install -m 755 "$tmp/ttyd" /usr/local/bin/ttyd
	sudo install -m 755 "$(find "$tmp" -name vhs -type f)" /usr/local/bin/vhs
	rm -rf "$tmp"
fi

npm install --global @openai/codex@latest @anthropic-ai/claude-code@latest

codex plugin marketplace add mattpocock/skills || true
codex plugin add mattpocock-skills@mattpocock || true
claude plugin marketplace add mattpocock/skills || true
claude plugin install mattpocock-skills@mattpocock || true
