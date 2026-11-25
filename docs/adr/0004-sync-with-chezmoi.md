---
status: accepted
---

# sshctl's files are synced by chezmoi, not by sshctl

The user wants the same Connections and Groups on every machine. sshctl gets no sync command. Its files are made easy for chezmoi, which the user already runs from a Git repo, to carry. A sync command would bring Git, credentials and merge conflicts into sshctl. chezmoi already handles all of those, and it also sets up a new machine in one step.

To make that work:
- **The config splits in two.** `config.json` holds what is the same everywhere: Groups, Connections' descriptions and Groups, the Provision defaults and Workspace preferences such as the prefix key. `state.json` holds what belongs to one machine: History, the Selected identity and key list, which Groups are collapsed, and list width. chezmoi manages `config.json` and `~/.ssh/sshctl_config`, never `state.json`.
- **One config path on every OS.** Both files live in `~/.config/sshctl/` (or `$XDG_CONFIG_HOME/sshctl/`) on Linux, macOS and Windows, so one chezmoi source path fits all of them. An existing file in the old OS-specific location is moved there once.
- **Saves write through symlinks.** chezmoi can make the targets symlinks into its source directory, so a change made in sshctl lands in the repo at once. A save still writes atomically, to the symlink's target.
- **The Include is offered at startup.** On a fresh machine `~/.ssh/sshctl_config` can arrive without `~/.ssh/config` including it. The Workspace offers to add the Include at startup (ADR-0002's flow), and `sshctl list` prints a hint.
- **IdentityFile is written with `~/`** for a key under the user's home, so synced Host blocks work on Windows, Linux and macOS.

## Consequences

- **sshctl's files are private, so chezmoi won't symlink them by default.** sshctl writes `config.json` and `sshctl_config` 0600, so `chezmoi add` stores them as `private_` files, and chezmoi's symlink mode never links a private file. The symlink setup has to clear the attribute (`chezmoi chattr -- -private`, as the Guide shows). sshctl still writes 0600 through the link.
- **Encrypted files in the repo that Provision applies need a `.chezmoiignore`.** When a user keeps sshctl's files encrypted in their public dotfiles repo, Provision applies that same repo on servers, which have no age key, and `chezmoi apply` would fail there. The repo has to ignore those files on machines without the key.

## Considered options

- **A built-in `sshctl sync` over Git:** more self-contained, but sshctl would own credentials, conflict handling and a Git dependency, duplicating what chezmoi does.
- **Keeping the files in the public dotfiles repo:** Host blocks reveal hostnames and users. The recommended setup is a small private repo (with symlinks), or chezmoi's encryption in the public one.
