# Repository Guidelines

## Project Structure & Module Organization

sshctl is one Go module (Go 1.25) with a single binary. Domain terms (Connection, Session, Tunnel, Workspace, Orphaned connection, Selected identity) are defined in `GLOSSARY.md`; the Workspace's stack is fixed by `docs/adr/0001-workspace-tui-stack.md`.

- `cmd/sshctl`: the CLI entry. `commands.go` declares every command once in the `commands` table (name, subcommand, arguments, one-line help, Experimental feature, handler), which drives both dispatch (`execute`) and `--help`, including `sshctl <command> --help`; a test checks the README's Commands block against it, so adding a command means one table entry and one README line. A handler returns `errUsage` for arguments that do not fit, and gets the command's usage printed. `options.go` is the one option parser (GNU convention, ADR-0011): a form names the options it takes, and its handler gets them parsed with its operands. `connectCommand` is `sshctl [-c] <alias> [-- <command>...]`, run when the first word is no command. `history.go`, `key.go`, `agent.go` and `knownhosts.go` are those commands; `connection.go` is `add`, `edit` and `remove`, which save through `catalog.Save` with a `Spec` and delete through `catalog.Delete`, as the Workspace form and `d` do (ADR-0008); `group.go` is `sshctl group`; `space.go` is `sshctl space` and the `--space` option of `list` and `group`; `tunnel.go` is `sshctl tunnel`; `json.go` is `sshctl info` and the records `--json` prints for `list`, `group list` and `space list`, whose field names are a compatibility promise (README); `bootstrap.go` is Bootstrap and `provision.go` Provision and `provision default`; `experimental.go` is `sshctl experimental`, which turns the Experimental features (Bootstrap, Provision) on and off; `workspace.go` wires the Workspace to the Connection catalog and `sessionCommand`, which runs an action tab as `sshctl <action> -- <alias>`. With no arguments sshctl opens the Workspace only when stdin and stdout are both terminals, and prints help otherwise.
- `internal/catalog`: the Connection catalog. Listing Connections (`Snapshot`, with `Entry`, `HasGroup`, `SpaceRef` and `GroupNames`), saving, deleting, regrouping them, reading and setting the Selected identity (global and per Space; `IdentityFor` picks the one a Connection uses), recording an open in History, saving collapsed Groups, a Connection's Tunnels (`tunnels.go`) and the dotfiles settings (global, per Space and per Group; `ProvisionRepo` resolves Group → Space → global) go through it, so the rename, drop and write-order rules live only here. `groups.go` holds the Group operations (add, rename, delete, move), each within one Space; `spaces.go` the Space operations (add, rename, delete, move, putting a Connection in a Space, the active Space). `resolve.go` is the one lookup of a Group or Space by name or number (`ResolveSpace`, `ResolveGroup`, `FindGroup`; `0` names ungrouped) that the CLI and the Workspace share. A Space is passed by name, and "" names the default Space.
- `internal/config`: the versioned JSON config, split per ADR-0004: `config.json` (Spaces, Groups, Connection metadata, Provision, `workspace` preferences; synced by chezmoi) and `state.json` beside it (`config.State`: History, Selected identity (global and per Space) and keys, collapsed Groups, list width, active Space; this machine only). Groups and Connections refer to the default Space as "" (ADR-0005), so renaming it rewrites only its record. `Load`/`Save` read and write both; migration from single-file and legacy configs. Both files live in `~/.config/sshctl/` on every OS (or `$XDG_CONFIG_HOME/sshctl/`), resolved by `Paths`; `MoveOld`, called before `Load`, moves them once from the old macOS/Windows folder.
- `internal/tunnel`: the Tunnel (ADR-0012), a saved local forward: its spec syntax (`Parse`, `String`) and the rules for a Connection's list (`Add` refuses a duplicate local port or name, `Remove` takes a name or port), shared by config, openssh, the catalog, the CLI and the Workspace's connection form.
- `internal/atomicfile`: the one atomic file write, through symlinks, that config and openssh share.
- `internal/openssh`: everything that runs or reads OpenSSH: ssh_config aliases (following `Include`), `ssh` argv (`ConnectArgs`, and `TunnelArgs` for Tunnels), Host block writes to `~/.ssh/sshctl_config` and the Include (`managed.go`), scripts run over ssh for Bootstrap and Provision (`remote.go`), key generation (`keygen.go`), `ssh-add`, `ssh-keygen -R`.
- `internal/platform`: opens a new OS terminal window running `ssh` for `group connect`, and reads the OS clipboard for the Workspace's right-click paste (`ReadClipboard`: Windows and macOS, not over ssh).
- `internal/workspace`: the Workspace TUI (Bubble Tea v2). `state.go` is the pure State, which lists only the active Space and keeps each Space's Active connection; `model.go` the Bubble Tea Model (construction, message and key routing, the Session, Group, Space and status intents); `mode.go` the Workspace's one mode (list, filter, scroll, drag, select, modal) and `setMode`, the only place it changes; `catalog.go` the `Catalog` interface the Workspace loads and saves through, and `Snapshot`; `mouse.go` mouse handling (list clicks, drag-and-drop of Connections and Group headers, pane selection, double-click and copy); `layout.go` layout geometry (`frame`: the rows of the Space bar, rules, pane header, tab line and status bar, and which a short terminal drops; list width, pane offset and size, visible list rows, the Tunnels section); `keys.go` the key bindings, declared once with their context and help text, which drive both key handling and the help screen, and follow the key rule of ADR-0007; `hints.go` the one hint-line helper (`hintsFor`, `Model.hint`), which builds every hint line from the key table where it shows (`bindings`, a modal's `keys()`, scroll mode's `scrollKeys`); `view.go` rendering; `term.go` the only importer of `charmbracelet/x/vt`; `sessions.go` the Session lifecycle (open, end, reconnect, close; the only writer of a Session's live/ended fields, owner of its runner); `runner.go` a Session's process in a pseudo-terminal (go-pty, ConPTY on Windows); `tunnels.go` the tunnels running in the background (start, stop, restart, failure; not Sessions: no tab, no pseudo-terminal), the Tunnels section's rows and its cursor; `groups.go` the Group header actions (add, rename, delete); `picker.go` the Space picker (`P`: a row per Space number, free ones too; switch, add, rename, delete, and move a Space to another number); `form.go` the add/edit connection form; `modal.go` the modals (form, prompts, confirmation, help screen). `selection.go` the text selected with the mouse in a Session's pane, which `model.go` copies; `paste.go` the right-click paste (both ADR-0006).

## Build, Test, and Development Commands

Run from the repository root. CI (`.github/workflows/ci.yml`) runs these, so run them before committing:

- `gofmt -l .` must print nothing (CI checks this on Ubuntu).
- `go vet ./...` (CI runs it on Ubuntu, macOS and Windows).
- `go test -race ./...` (CI runs it on Ubuntu, macOS and Windows).

CI does not cross-vet, so also run `GOOS=windows go vet ./...` locally to catch Windows-only build errors before pushing.
- `go run ./cmd/sshctl` launches the CLI; in a terminal with no arguments it opens the Workspace.

Use Go 1.25 only, and set `GOTOOLCHAIN=local` if `go get` tries to upgrade. Newer Lip Gloss releases need Go 1.26, so dependency versions in `go.mod` stay pinned.

## Coding Style & Naming Conventions

Follow standard Go conventions: tabs for indentation (as applied by `gofmt`), mixed-case exported identifiers, and short lowercase package names. Keep packages focused and avoid stuttering names (for example, `ssh.Client`, not `ssh.SSHClient`). Name tests after behavior or the unit under test, such as `TestParseConfig`.

## Testing Guidelines

Use Go’s built-in `testing` package and colocate tests with implementation. Prefer table-driven tests for multiple input cases. Workspace tests go through these seams:

- **State** (`state_test.go`): ordering, open/active/ended, filter and Reload on the pure State, with no terminal.
- **Golden layouts** (`model_test.go`): `newTestModel`, `press` and `golden` render the whole Workspace to `testdata/*.golden`. After an intended layout change, regenerate with `go test ./internal/workspace -update` and review the golden diff before committing.
- **Real-process Sessions** (`session_test.go`): `newSessionModel(t, script{sh, cmd})` runs `sh` on POSIX and `cmd.exe` on Windows in place of `ssh`, and `pump` feeds runner events until the screen matches. Give every new Session test a `cmd` variant; a test without one is skipped on Windows.
- **Emulator-only pane** (`selection_test.go`): `newPaneModel(t, out)` opens a Session whose runner is just an emulator showing `out`, with no process, so mouse selection and paste tests are deterministic on every platform; `paneModel` also takes `Options` and returns what the emulator sends to its program.
- **Recording fake Command** (`fixture_test.go`): `newRecordingModel(t, entries, fail...)` returns a Workspace and a `recorder` of every `Command` call (alias, Launch) and every alias its `fakeCatalog` recorded as opened. Its Command runs the test binary with no tests, which exits at once on every platform, and fails for the aliases in `fail`. Use it for tests about which Sessions start, not what they print.
- **OpenSSH** (`internal/openssh`): tests write temporary ssh_config trees.
- **Connection catalog** (`internal/catalog`): tests use a temporary sshctl config and ssh_config tree (`newTree`). Workspace tests replace it with `fakeCatalog` (`fixture_test.go`) through `Options.Catalog`, which every Workspace needs.

Tests never start the real `ssh`. `Options.Command`, `Options.Resolve` (the pane header's `ssh -G`) and `Options.Catalog` are the seams for replacing it and the config.

## Commit & Pull Request Guidelines

Use concise imperative commit subjects (for example, `Add SSH config parsing`). Pull requests should explain the change and rationale, list the validation commands and results, and link the issue. Include screenshots only when a change adds a visual interface.

Ticket branches start from `main` and open a pull request back into it. Work spanning several tickets gets a feature branch off `main` (as `connection-bootstrap` was for #36); its ticket branches start from it and merge back into it, and the feature branch then goes to `main` as one pull request. Before handing off, merge the base branch's current tip into the ticket branch and re-run the checks.

### Releases

A release is a milestone titled with its version tag (`v0.6.0`). Put each ticket the release needs in it. When the milestone has no open issues left (or is closed by hand) and CI has passed on `main`, `.github/workflows/milestone-release.yml` closes it, tags `main` with its title and runs `release.yml`, which builds the binaries and publishes the release with generated notes. Issues close when their fix reaches `main`, so the pull request that brings a feature branch to `main` must say `Closes #N` for every issue it finishes, the parent issue too. Never reuse a version: a milestone whose tag exists is not released again.

## Security & Configuration

Do not commit credentials, private keys, or machine-specific configuration. Keep secrets in environment variables or an approved secret store, and document required configuration with safe example values.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `jackykornet/sshctl`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default triage labels are used as-is (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
