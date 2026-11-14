---
status: accepted
---

# The Workspace writes Host blocks only to its own included ssh_config file

Until now sshctl only read ssh_config. To add, edit and delete Connections from the Workspace, it now writes Host blocks too, but only to `~/.ssh/sshctl_config`, a file it owns. The first time, it asks to add `Include sshctl_config` to the top of the user's `~/.ssh/config`. That file is otherwise never rewritten, so a user's comments, ordering and complex blocks cannot be damaged. A Connection whose Host block lives anywhere else stays the user's: the Workspace edits only its description and group.

## Considered options

- **Metadata only (as `sshctl add/edit`):** safest, but you cannot create a Connection without editing ssh_config by hand.
- **Edit `~/.ssh/config` in place:** every host would be editable, but sshctl would need a lossless ssh_config rewriter (comments, `Match`, `Include`, quoting) and could corrupt the file people care about most.

## Consequences

- The Include must come before the first `Host`/`Match` so it applies globally. Because OpenSSH uses the first value it sees, sshctl's blocks win over a later block with the same alias, so adding or renaming refuses aliases that already exist anywhere in ssh_config.
- sshctl writes only HostName, User, Port and IdentityFile. It keeps other directives a user adds inside its blocks, but drops comments when it rewrites the file.
- Values are validated (one word, no quotes, `#`, `=` or newlines, port 1–65535), so a form field cannot inject a directive such as `ProxyCommand`.
- The previous `~/.ssh/config` is kept as `config.sshctl-backup` when the Include line is added.
