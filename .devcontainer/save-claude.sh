#!/bin/sh
# One-time migration: snapshot Claude history and settings from a container
# built before the Claude config volume existed, so post-create.sh can
# restore it into the volume after the rebuild. Credentials are left out
# (log in once after the rebuild); the snapshot is gitignored and deleted
# as soon as it is restored.
set -eu

out="$(dirname "$0")/.claude-snapshot.tgz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cp -a "$HOME/.claude/." "$tmp/"
[ -f "$HOME/.claude.json" ] && cp -a "$HOME/.claude.json" "$tmp/.claude.json"
rm -f "$tmp/.credentials.json"
rm -rf "$tmp/ide" "$tmp/session-env" "$tmp/shell-snapshots"

tar -czf "$out" -C "$tmp" .
echo "Wrote $out; rebuild the container now."
