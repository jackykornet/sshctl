# sshctl

`sshctl` is a cross-platform Go CLI for managing connection metadata while delegating SSH work to your installed OpenSSH tools. It never stores private keys or passphrases.

## Install

Download a binary or archive for Windows, macOS, or Linux from the [GitHub Releases](https://github.com/jackykornet/sshctl/releases) page. The Windows executable is named `sshctl_<version>_windows_<architecture>.exe`; macOS and Linux have similarly named executables. Put the executable somewhere on your `PATH`. The archives contain the same executable.

You can also build from source with Go 1.25 or newer:

```sh
go install ./cmd/sshctl
```

You need OpenSSH client tools (`ssh`, `ssh-add`, and `ssh-keygen`) available on `PATH`.

## Commands

```text
sshctl list
sshctl connect <alias> [--claude]
sshctl add <alias>
sshctl edit <alias>
sshctl remove <alias>
sshctl group connect <group>
sshctl history
sshctl key list
sshctl key use <key-path|none>
sshctl key add <private-key-path>
sshctl agent ensure
sshctl known-hosts list
sshctl known-hosts remove <host>
sshctl config show
```

Aliases come from `~/.ssh/config` and any files it pulls in with `Include` (wildcard `Host` patterns are skipped); saved connection descriptions and groups are sshctl metadata only. `add` and `edit` prompt for a description and group. `connect` runs `ssh <alias>` (and appends `claude` remotely with `--claude`). A selected sshctl key adds `-i <path>`; with no override, OpenSSH chooses identities using its normal config and defaults.

`key list` lists public keys and certificates found in `~/.ssh` and registered external key paths. `key add` registers a key path; `key use` selects the identity used by sshctl. `agent ensure` inspects `ssh-add -l` and asks before adding the selected key. It does not start an agent automatically, and does not perform a signing challenge to inspect loaded keys. On Windows, use the OpenSSH Authentication Agent service and start it yourself if unavailable; normal connections do not require elevation.

`known-hosts remove` asks before invoking `ssh-keygen -R`, which handles hashed entries. `known-hosts list` displays the host field as stored (hashed names remain hashed).

Group launching starts a separate terminal for each saved connection. The CLI supports Windows Terminal (or `cmd start` fallback), macOS Terminal, and Linux GNOME Terminal, Konsole, Xfce Terminal, or `x-terminal-emulator`. Availability depends on the host's installed terminal; the command reports an actionable error when none is found.

## Configuration and security

The versioned JSON file is stored in the OS user config directory under `sshctl/config.json` (for example, `%AppData%\\sshctl\\config.json` on Windows and `~/.config/sshctl/config.json` on Linux). Files are written with owner-only permissions where supported. If `~/.sshctl/connections.json` or `~/.sshyeah/connections.json` exists and no new config exists, sshctl migrates recognized alias, description, and group fields and leaves the source untouched.

Private keys and passphrases are never read into sshctl config. OpenSSH and `ssh-agent` handle key use and in-memory caching. `config show` displays config paths and alias count without printing credentials or key contents.

## Platform support

The release workflow builds Windows, macOS, and Linux binaries for amd64 and arm64. Platform behavior still depends on locally installed OpenSSH and terminal applications. This workspace has only been executed on Linux; other platforms should be considered unverified until CI runs successfully.

To publish an existing tag, run the **Release** workflow from GitHub Actions and enter the tag (for example, `v0.1.0`). New `v*` tags publish automatically.

## Development

```sh
go test ./...
go vet ./...
```
