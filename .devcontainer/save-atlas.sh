#!/bin/sh
# One-time migration: snapshot atlas config, memory and notes from a container
# built before the atlas volumes and the notes mount existed, so post-create.sh
# can restore them after the rebuild. Models are left out (atlas downloads them
# again); the snapshot is gitignored and deleted as soon as it is restored.
set -eu

out="$(dirname "$0")/.atlas-snapshot.tgz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

mkdir -p "$tmp/config" "$tmp/data/state" "$tmp/notes"
cp -a "$HOME/.config/atlas/." "$tmp/config/"
state="$HOME/.local/share/atlas/state"
# Copy each live SQLite database through the backup API so the copy is consistent.
for db in atlas.sqlite memory.sqlite; do
	python3 -c 'import sqlite3,sys; s=sqlite3.connect(sys.argv[1]); d=sqlite3.connect(sys.argv[2]); s.backup(d); d.close()' \
		"$state/$db" "$tmp/data/state/$db"
done
for f in "$state"/*; do
	case "$(basename "$f")" in
	*.sqlite | *.sqlite-wal | *.sqlite-shm | *.lock | *.log) ;;
	*) cp -a "$f" "$tmp/data/state/" ;;
	esac
done
[ -d "$HOME/sshctl-notes" ] && cp -a "$HOME/sshctl-notes/." "$tmp/notes/"

tar -czf "$out" -C "$tmp" .
echo "Wrote $out; create the notes folder on your computer, then rebuild the container."
