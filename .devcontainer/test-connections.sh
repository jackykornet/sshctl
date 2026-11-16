#!/bin/sh
# Writes a set of test Connections into this devcontainer's home so sshctl
# can be tried by hand: user-owned and Managed connections, Groups, an
# ungrouped one, an Orphaned connection, a wildcard Host that must be
# skipped, and an Include'd file.
#
# Nothing listens on the test hosts, so Sessions end quickly (exit 255),
# which is enough to try tabs, ended Sessions and reconnect.
#
# Usage: .devcontainer/test-connections.sh [--force]
#   --force  overwrite existing files (each is backed up to <file>.bak first)
set -eu

force=no
[ "${1:-}" = "--force" ] && force=yes

ssh_dir="$HOME/.ssh"
cfg_dir="${XDG_CONFIG_HOME:-$HOME/.config}/sshctl"
mkdir -p "$ssh_dir/config.d" "$cfg_dir"
chmod 700 "$ssh_dir"

write() { # write <path>; content on stdin
	if [ -e "$1" ]; then
		if [ "$force" != yes ]; then
			echo "skip: $1 exists (use --force to overwrite)" >&2
			cat >/dev/null
			return
		fi
		cp "$1" "$1.bak"
	fi
	cat >"$1"
	chmod 600 "$1"
	echo "wrote $1"
}

write "$ssh_dir/config" <<'EOF'
Include sshctl_config
Include config.d/*

# Production (user-owned Host blocks; sshctl only edits description/group)
Host web-1
  HostName 127.0.0.1
  Port 2201
  User deploy

Host web-2
  HostName 127.0.0.1
  Port 2202
  User deploy

Host db-main
  HostName db-main.example.invalid
  User postgres

# Staging
Host staging-web
  HostName 127.0.0.1
  Port 2301

# Ungrouped
Host bastion
  HostName bastion.example.invalid
  User admin

# Defaults for every host; sshctl must skip this wildcard block.
Host *
  ConnectTimeout 3
  ServerAliveInterval 30
EOF

write "$ssh_dir/config.d/work" <<'EOF'
# Reached through Include config.d/*
Host work-jump
  HostName jump.work.example.invalid
  User jacky
EOF

write "$ssh_dir/sshctl_config" <<'EOF'
Host lab-pi
  HostName 127.0.0.1
  User pi
  Port 2401

Host lab-nas
  HostName nas.lab.example.invalid
  User admin
EOF

write "$cfg_dir/config.json" <<'EOF'
{
  "version": 1,
  "connections": {
    "web-1": { "alias": "web-1", "description": "Frontend 1", "group": "prod" },
    "web-2": { "alias": "web-2", "description": "Frontend 2", "group": "prod" },
    "db-main": { "alias": "db-main", "description": "Primary Postgres", "group": "prod" },
    "staging-web": { "alias": "staging-web", "description": "Staging frontend", "group": "staging" },
    "lab-pi": { "alias": "lab-pi", "description": "Raspberry Pi", "group": "lab" },
    "lab-nas": { "alias": "lab-nas", "group": "lab" },
    "work-jump": { "alias": "work-jump", "description": "Work jump host" },
    "old-server": { "alias": "old-server", "description": "Decommissioned box", "group": "prod" }
  },
  "history": ["web-1", "lab-pi"],
  "workspace": {
    "collapsedGroups": ["staging"]
  }
}
EOF

cat <<'EOF'

Test Connections:
  prod     web-1, web-2, db-main, old-server (Orphaned: no Host block)
  staging  staging-web (starts collapsed)
  lab      lab-pi, lab-nas (Managed: in ~/.ssh/sshctl_config)
  -        bastion, work-jump (from ~/.ssh/config.d/work)
Try: go run ./cmd/sshctl        (Workspace)
     go run ./cmd/sshctl list
EOF
