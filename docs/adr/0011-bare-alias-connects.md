---
status: accepted
---

# A bare alias connects; built-in commands win

The user wants to reach a host by typing `sshctl web01`, as with `ssh web01`. sshctl keeps its subcommands (`list`, `space`, `group`, `key`, ...), so a first word that is not a built-in command is taken as a Connection's alias and sshctl connects to it. A built-in command always wins: an alias named `list` or `key` is reached with `sshctl -c <alias>` (`--connect`), which never means a command and is the form scripts should use. `connect` is removed. A remote command is run with `sshctl <alias> -- <command...>`, exiting with the command's exit code. Every option follows the GNU convention on every OS: `--long`, a few short forms (`-c -s -g -y -h`), `--opt value` and `--opt=value`, accepted anywhere after the command name, and `--` ends the options.

## Considered options

- **Keep `sshctl connect <alias>` only:** no ambiguity, but the most common action stays the longest to type.
- **Flags only, with no subcommands (`sshctl --list`, `sshctl --group-rename prod --to production`):** a bare word is always an alias, so nothing can clash. But about thirty mutually exclusive action flags, a second flag for two-value actions and one flat help list fit single-purpose tools such as `ssh` or `curl`, not a tool with this many actions. git, gh and chezmoi, which sshctl's users already run, use subcommands.

## Consequences

- An alias equal to a built-in command name cannot be reached bare. `sshctl list` marks such aliases and the Workspace's add and edit form warns about them, pointing to `-c`. The name is not refused, since plain `ssh` still reaches it.
- Adding a built-in command later silently hides an alias of that name from `sshctl <word>`; scripts using `-c` are unaffected. A new command's name is a breaking change for anyone with that alias, and goes in the release notes.
- A first word that is neither a built-in command nor a Connection's alias is refused, not passed to `ssh`, so a typo is not tried as a host name. A host matched only by a wildcard `Host` pattern in ssh_config is no Connection, so it needs `sshctl -c <name>`.
- If executable extensions (`sshctl-foo`) are added, they come after aliases: built-in command, then alias, then extension.
- The Workspace starts action tabs as `sshctl <action> -- <alias>`, which is unchanged.
