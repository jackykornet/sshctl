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
sshctl
sshctl list
sshctl connect <alias> [--claude]
sshctl add <alias>
sshctl edit <alias>
sshctl remove <alias>
sshctl group list
sshctl group add <name>
sshctl group rename <old> <new>
sshctl group delete <name> [--yes]
sshctl group move <name> <position>
sshctl group connect <group>
sshctl history
sshctl key list
sshctl key use <key-path|none>
sshctl key add <private-key-path>
sshctl agent ensure
sshctl known-hosts list
sshctl known-hosts remove <host>
sshctl bootstrap <alias>
sshctl provision <alias>
sshctl provision default [<url|none> | --unset]
sshctl config show
```

Aliases come from `~/.ssh/config` and any files it pulls in with `Include` (wildcard `Host` patterns are skipped); a Connection's description and group are sshctl metadata only. `list` shows the same Connections as the Workspace: the ssh_config aliases in ssh_config order, then Orphaned connections (metadata whose alias is no longer in ssh_config) sorted by alias and marked `(orphaned)`. `add` and `edit` prompt for a description and group; an empty answer keeps the current value, and a Connection with neither keeps no metadata. The group must already exist: `add` and `edit` refuse an unknown one and tell you to run `sshctl group add`. `connect` runs `ssh <alias>` (with `--claude`, `ssh -t <alias> claude`, so claude gets a remote terminal). A selected sshctl key adds `-i <path>`; with no override, OpenSSH chooses identities using its normal config and defaults.

`key list` lists public keys and certificates found in `~/.ssh` and registered external key paths. `key add` registers a key path; `key use` selects the identity used by sshctl. `agent ensure` inspects `ssh-add -l` and asks before adding the selected key. It does not start an agent automatically, and does not perform a signing challenge to inspect loaded keys. On Windows, use the OpenSSH Authentication Agent service and start it yourself if unavailable; normal connections do not require elevation.

`known-hosts remove` asks before invoking `ssh-keygen -R`, which handles hashed entries. `known-hosts list` displays the host field as stored (hashed names remain hashed).

Groups exist from `group add` until `group delete`, even with no Connections, and are listed in the order you set. `group list` shows each Group's number (its position, counting from 1, as in the Workspace headers), name, member count and dotfiles repo for Provision (`unset` when the global default applies). `group rename` moves every Connection with the Group and keeps its position and collapsed state; it refuses a name another Group has. A group name may not be only digits, since numbers stand for positions. `group delete` asks `Delete group X? N Connections become ungrouped.` first (`--yes` skips the question); it never deletes a Connection or a Host block. `group move <name> <position>` puts the Group at that position, shifting the others. The CLI takes Group names only, never numbers, so scripts keep working when Groups are reordered.

Group launching starts a separate terminal for each Connection in the group. The CLI supports Windows Terminal (or `cmd start` fallback), macOS Terminal, and Linux GNOME Terminal, Konsole, Xfce Terminal, or `x-terminal-emulator`. Availability depends on the host's installed terminal; the command reports an actionable error when none is found.

### Bootstrap

`bootstrap <alias>` makes a Connection reachable with your selected key (`sshctl key use`), in your terminal:

1. For a Connection in `sshctl_config`, it writes the HostName, User and Port that `ssh -G <alias>` resolves into its Host block, if they are missing.
2. It adds the key's `.pub` to `~/.ssh/authorized_keys` on the host over plain `ssh <alias>` (no `ssh-copy-id`), so you type your password once. A key that is already there is not added again, and `~/.ssh` and the file are made private (700 and 600).
3. It proves that the key alone logs in, with a fresh `ssh` that has BatchMode on and password login off.
4. Only then does it set `IdentityFile` in the Host block, written as `~/.ssh/...` when the key is in your home directory.

If the check in step 3 fails, nothing after step 1 changes and sshctl says why. A key with a passphrase must be in the agent for the check (`sshctl agent ensure`). A host without a POSIX `sh` fails before anything is installed. For a host defined elsewhere in your ssh_config, Bootstrap only installs the key and leaves the Host block as it is. With no selected key, Bootstrap first creates one: `ssh-keygen` makes an ed25519 key at `~/.ssh/id_ed25519`, or at `~/.ssh/sshctl_ed25519` when `id_ed25519` already exists (an existing key is never overwritten), asks you for a passphrase (leave it empty for none), and the new key becomes the selected key. Bootstrap never changes the host's SSH server settings, and never runs Provision. Running it again is safe.

### Provision

`provision <alias>` applies your dotfiles to the Connection's host with [chezmoi](https://www.chezmoi.io/), in your terminal over `ssh <alias>` (with the selected key's `-i`). If chezmoi is already on the host (on `PATH` or in `~/.local/bin`), it runs `chezmoi update --apply`. Otherwise it installs chezmoi into `~/.local/bin` with chezmoi's install script, fetched with `curl -fsLS` or, when curl is missing, `wget -qO-`, and runs `init --apply <repo>`; that is the same as running `sh -c "$(curl -fsLS get.chezmoi.io)" -- -b "$HOME/.local/bin" init --apply <repo>` on the host. Provision runs only chezmoi: anything else, such as installing tools with mise, is up to your dotfiles. It never uses sudo and writes only inside the remote home. It never runs on its own and is not part of connecting.

Provision stops before changing anything when no repo is set, when the setting is `none`, when the host has no POSIX `sh`, or when it has neither curl nor wget. A failure while applying, such as a tool failing to build, is shown as it happens and sshctl exits with its exit code. The POSIX check is a separate ssh connection, so a host you log in to with a password asks for it twice.

The repo is a global setting in sshctl's config: `provision default <url>` sets it, `provision default none` refuses Provision for every Connection, `provision default --unset` clears it, and `provision default` with no arguments shows it:

```text
sshctl provision default https://github.com/you/dotfiles.git
sshctl provision default
global: https://github.com/you/dotfiles.git
```

## Workspace

Run `sshctl` with no arguments in a terminal to open the Workspace: the connection list on the left, the tabs of the active connection on the right, and a status bar at the bottom. (When output is piped or redirected, `sshctl` prints help instead.)

Each tab is a Session: an `ssh <alias>` process in its own pseudo-terminal, started with the selected identity at the moment the tab opens. Open connections are marked green and keep their place in the list. A tab whose `ssh` has exited stays open with its exit code; press `r` to reconnect or `x` to close it. Quitting with live Sessions asks first and then ends them.

Keys go to the remote session. Workspace commands start with the prefix key (`Ctrl+B` by default; press it twice to send it to the session):

| Keys | Action |
| --- | --- |
| `↑`/`↓`, `Enter` | Move in the list, open or switch to a connection |
| `Enter`/`Space`, `←`/`→` | On a group header: collapse or expand the group (`←` on a member collapses its group) |
| `Shift+↑`/`Shift+↓` (or `K`/`J`) | On a group header: move the group up / down; the new order is saved |
| `/` | Filter the list by alias, description, or group |
| `Alt+←`/`Alt+→` | Previous / next tab |
| `Alt+↑`/`Alt+↓` | Previous / next open connection |
| prefix `n` / `w` | New tab / close tab |
| prefix `c` | New tab running `claude` on the remote host (marked `claude` in the tab bar) |
| prefix `i` / `p` | Bootstrap / Provision the connection in its own labelled tab (Provision asks first, and refuses at once when the connection's group is set to `none`) |
| prefix `g` | Open every closed connection in the group under the cursor (`group connect` still opens separate windows) |
| prefix `a` / `e` / `d` (or `a` / `e` / `d` in the list) | Add / edit / delete the connection under the cursor; on a group header, add / rename / delete a group |
| prefix `m` | Move the connection to a group: type an existing group name, or clear it to remove the connection from its group |
| prefix `←`/`→`, `1`–`9` | Switch tab |
| prefix `Tab` | Move focus between the list and the terminal |
| prefix `[` | Scroll back through the tab's history (`q` to leave) |
| prefix `b` / `r` | Hide the list / reload connections from disk |
| prefix `?` / `q` | Help / quit |

### Adding, editing and deleting connections

`a` opens a form for a new connection: alias, HostName, User, Port, identity file, description and group (the group under the cursor is filled in). A group that does not exist yet is created after you confirm `Create group X?`. sshctl writes the Host block to its own file, `~/.ssh/sshctl_config`, and never rewrites your hand-written `~/.ssh/config`. The first time, it asks to add `Include sshctl_config` to the top of `~/.ssh/config` so OpenSSH reads the file, and keeps the previous file as `~/.ssh/config.sshctl-backup`.

`e` edits the connection under the cursor (or the active one). For connections in `sshctl_config` you can change every field, including the alias. For hosts defined elsewhere in your ssh_config, only the description and group can be edited. `d` deletes a connection's Host block from `sshctl_config` along with its description and group, after asking first. For other hosts it removes only the description and group. A connection with open tabs cannot be renamed or deleted. Options you add by hand inside an `sshctl_config` Host block are kept when sshctl rewrites the file; comments are not.

### Groups

Once any connection has a group, the list shows each group (alphabetically, with `ungrouped` last) under a header showing its member count, how many of its members are open (`●2`), and whether any has unseen activity, so a collapsed group (`▸`) still shows what is going on inside it. Collapsed groups are remembered between runs.

On a group header, `a` asks for a new group name (added after the others), `e` renames the group (its connections move with it), and `d` deletes it after asking `Delete group X? N connections become ungrouped.`; no connection is deleted. Names follow the same rules as `sshctl group`: unique and not only digits.

### Mouse and settings

The mouse selects connections and tabs and scrolls history. Click a group header to collapse or expand it, drag a group header onto another group's header to move the group to that position, and drag a connection onto any row of another group (its header or one of its members) to move it there. Release off the list to cancel. Group changes are saved to sshctl's metadata, the same as `sshctl edit`. Full-screen programs such as `vim` receive mouse events when they ask for them. Hold Shift while dragging to select text with your own terminal. Remote programs can copy to your clipboard with OSC 52 (for example from tmux or vim); they cannot read it.

Optional settings go in a `workspace` object in the config file:

```json
"workspace": { "prefixKey": "ctrl+a", "scrollbackLines": 20000 }
```

The list width is this machine's own state: sshctl keeps it in `state.json`, not `config.json`.

The Workspace needs Windows 10 version 1809 or newer for its pseudo-terminals; the other commands do not.

## Configuration and security

sshctl keeps two versioned JSON files in `~/.config/sshctl/` on Linux, macOS and Windows alike (`%USERPROFILE%\.config\sshctl\` on Windows), or in `$XDG_CONFIG_HOME/sshctl/` when `XDG_CONFIG_HOME` is set to an absolute path: `config.json`, the part to sync between machines, and `state.json`, this machine's own state. One chezmoi source path, `dot_config/sshctl/config.json`, therefore fits every OS. Older versions used `~/Library/Application Support/sshctl/` on macOS and `%AppData%\sshctl\` on Windows. sshctl moves both files from there once and says so. If both folders hold a config, it uses the new one, leaves the old one alone and says it can be removed. `sshctl config show` prints both paths. Files are written with owner-only permissions where supported. If `~/.sshctl/connections.json` or `~/.sshyeah/connections.json` exists and no new config exists, sshctl migrates recognized alias, description, and group fields and leaves the source untouched.

The only SSH config sshctl writes is `~/.ssh/sshctl_config` (owner-only), plus the one `Include` line it asks before adding to `~/.ssh/config`. It checks every value so a field cannot add other directives. Private keys and passphrases are never read into sshctl config. OpenSSH and `ssh-agent` handle key use and in-memory caching. `config show` displays config paths and alias count without printing credentials or key contents.

## Syncing with chezmoi

sshctl has no sync command. To keep the same Connections and Groups on every machine, let [chezmoi](https://www.chezmoi.io/) carry two files:

| Sync | File | Holds |
| --- | --- | --- |
| Yes | `~/.config/sshctl/config.json` | Groups, Connections' descriptions and Groups, Provision repos, `workspace` settings |
| Yes | `~/.ssh/sshctl_config` | The Host blocks sshctl writes |
| Never | `~/.config/sshctl/state.json` | This machine's History, selected key and key list, collapsed Groups, list width |
| Never | Private keys (`~/.ssh/id_ed25519` and the like) | Copy or create them on each machine yourself |

Host blocks name your hosts, users and ports, so don't put `sshctl_config` in a public repo unencrypted. Use a small private repo of its own (recommended), or chezmoi's encryption in your public dotfiles repo.

### Recommended: a private repo with symlinks

A second chezmoi source, with its own config, keeps these files apart from your dotfiles. In symlink mode chezmoi makes the targets symlinks into its source directory. sshctl saves through the symlink, so every change lands in the repo's working tree at once.

Create the second source's config and a shortcut for it (bash or zsh; put the alias in your shell's rc file):

```sh
mkdir -p ~/.config/chezmoi-sshctl
cat > ~/.config/chezmoi-sshctl/chezmoi.toml <<'EOF'
sourceDir = "~/.local/share/chezmoi-sshctl"
mode = "symlink"
EOF
alias sshctl-sync='chezmoi --config ~/.config/chezmoi-sshctl/chezmoi.toml'
```

Then add the two files, push them to a new private repo (here `git@github.com:you/sshctl-sync.git`), and turn the files into symlinks:

```sh
sshctl-sync init
sshctl-sync add ~/.config/sshctl/config.json ~/.ssh/sshctl_config
sshctl-sync chattr -- -private ~/.config/sshctl/config.json ~/.ssh/sshctl_config
sshctl-sync apply
sshctl-sync git -- add .
sshctl-sync git -- commit -m "Add sshctl config"
sshctl-sync git -- remote add origin git@github.com:you/sshctl-sync.git
sshctl-sync git -- push -u origin HEAD
```

The `chattr` step is needed because sshctl writes both files owner-only, so `chezmoi add` marks them `private_`, and chezmoi never symlinks private files. sshctl still writes owner-only through the symlink.

Day to day, change Connections or Groups in sshctl as usual, then commit and push:

```sh
sshctl-sync git -- commit -am "Update Connections"
sshctl-sync git -- push
```

On each other machine, pull the change, then press `^B r` in a running Workspace to reload the list:

```sh
sshctl-sync update
```

### Setting up a new machine

Install sshctl and chezmoi, create `~/.config/chezmoi-sshctl/chezmoi.toml` and the `sshctl-sync` alias as above, then clone and apply:

```sh
sshctl-sync init --apply git@github.com:you/sshctl-sync.git
```

If sshctl has run on this machine before, chezmoi asks before replacing its `config.json`. `state.json` is not touched, so this machine keeps its own History and keys. Choose this machine's key with `sshctl key use <key-path>`, or let Bootstrap create one.

`~/.ssh/config` on a new machine usually lacks the `Include sshctl_config` line, so ssh cannot see the synced hosts yet. The Workspace asks at startup: `Add "Include sshctl_config" to the top of ~/.ssh/config? (y/n)`. Answer `y` (the previous file is kept as `~/.ssh/config.sshctl-backup`). Answering `n` leaves the file alone for the rest of the session, and `sshctl list` prints a hint until the line is there. If your dotfiles already manage `~/.ssh/config`, add the line there instead, so chezmoi doesn't undo it.

sshctl writes `IdentityFile` as `~/.ssh/...` for a key in your home directory, so one Host block works on Linux, macOS and Windows as long as each machine has a key at that path.

On Windows, chezmoi can only create symlinks when your account may create them (Developer Mode, or an administrator shell). Without symlinks, use the encrypted setup's `re-add` step below to copy sshctl's changes into the source before committing.

### Alternative: encrypted in your public dotfiles repo

chezmoi can encrypt the two files with [age](https://www.chezmoi.io/user-guide/encryption/age/). Create a key, and keep it out of the repo and safe, since it is the only way to decrypt the files:

```sh
chezmoi age-keygen --output ~/.config/chezmoi/key.txt
```

Put the printed public key in `~/.config/chezmoi/chezmoi.toml`. `encryption` must come before any `[section]`:

```toml
encryption = "age"
[age]
    identity = "~/.config/chezmoi/key.txt"
    recipient = "age1..."
```

Then add the files encrypted:

```sh
chezmoi add --encrypt ~/.config/sshctl/config.json ~/.ssh/sshctl_config
```

chezmoi never symlinks an encrypted file, so changes made in sshctl stay in the target until you copy them back. `re-add` re-encrypts them:

```sh
chezmoi re-add ~/.config/sshctl/config.json ~/.ssh/sshctl_config
chezmoi git -- commit -am "Update Connections"
chezmoi git -- push
```

Elsewhere, `chezmoi update` and `^B r` work as above. A new machine needs `key.txt` and the config above before `chezmoi init --apply <your-dotfiles-repo>`.

Provision applies the same dotfiles repo on your servers, which have no age key, so `chezmoi apply` would fail there. Skip the two files on machines without the key with a `.chezmoiignore` in the repo:

```text
{{ if not (stat (joinPath .chezmoi.homeDir ".config/chezmoi/key.txt")) }}
.config/sshctl/config.json
.ssh/sshctl_config
{{ end }}
```

## Platform support

The release workflow builds Windows, macOS, and Linux binaries for amd64 and arm64.

CI runs `go vet` and the test suite on the GitHub-hosted Ubuntu, macOS, and Windows runners (Linux and Windows on amd64, macOS on arm64). The other three targets are only cross-compiled. The Workspace's Session tests start real processes in pseudo-terminals on all three runners, with `sh` (or `cmd.exe` on Windows, through ConPTY) standing in for `ssh`. A few Session tests rely on POSIX-only tools and are skipped on Windows.

CI does not run a real OpenSSH client or open terminal windows, so actual connections and `group connect`'s terminal launching depend on the locally installed OpenSSH and terminal applications and are not verified by CI.

The Workspace needs Windows 10 version 1809 or newer, because it uses ConPTY for its pseudo-terminals. The other commands do not.

To publish an existing tag, run the **Release** workflow from GitHub Actions and enter the tag (for example, `v0.1.0`). New `v*` tags publish automatically.

## Development

```sh
gofmt -l .
go vet ./...
go test -race ./...
```

After an intended change to the Workspace layout, regenerate its golden files with `go test ./internal/workspace -update` and review the diff.
