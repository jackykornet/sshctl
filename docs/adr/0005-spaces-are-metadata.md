---
status: accepted
---

# Spaces are sshctl metadata; aliases stay one namespace

The user wants to keep Connections for different customers and projects apart, such as homelab, customer1 and prive, and to switch between them in the Workspace. sshctl calls each of these a **Space**. "Workspace" already names the full-screen view, so the new term avoids it. A Space is an ordered record in `config.json`, like a Group (ADR-0003). Every Group and every Connection names the Space it belongs to. Connections not assigned to one belong to the default Space, which can be renamed but not deleted. Host blocks stay where they are: Managed connections in `~/.ssh/sshctl_config` and the user's own in their ssh_config. So an alias still names exactly one host everywhere, and plain `ssh web01`, `scp`, `rsync` and editors' remote extensions keep working without sshctl. Which Space is active, and which Selected identity each Space uses, belong to one machine and go in `state.json` (ADR-0004).

## Considered options

- **One ssh_config file per Space, run with `ssh -F`:** this would allow the same alias, such as `web01`, in two Spaces. But only sshctl would reach those hosts: every other tool would need `-F <space file>`, the user's own `~/.ssh/config` defaults would have to be Included from each Space file, and the CLI would need `space/alias` everywhere. Clashing names between customers have not happened yet, so the cost is not worth paying now. This can be revisited if they do.
- **Space as a filter over Groups (a Group belongs to a Space, Connections only through their Group):** this would leave ungrouped Connections without a Space and couple moving a Connection between Spaces to Group membership.

## Consequences

- The config is migrated once: a default Space is created, and every existing Group and Connection goes into it.
- Group names are unique only within a Space, and Group numbers count within the active Space. Collapsed Groups in `state.json` are keyed `space/group`.
- `sshctl connect <alias>` is unchanged. `--space` selects the Space for `list` and `group` commands. A Group name that exists in several Spaces, or a Group number when there are several Spaces, needs `--space`.
- A dotfiles repo is resolved Group → Space → global, and the Selected identity Space → global.
- History stays global.
