# Repository Guidelines

## Project Structure & Module Organization

sshctl is one Go module (Go 1.25) with a single binary. Domain terms (Connection, Session, Workspace, Orphaned connection, Selected identity) are defined in `GLOSSARY.md`; the Workspace's stack is fixed by `docs/adr/0001-workspace-tui-stack.md`.

- `cmd/sshctl`: the CLI entry. `commands.go` dispatches subcommands; `group.go` is `sshctl group`; `bootstrap.go` is Bootstrap and `provision.go` Provision and `provision default`; `workspace.go` wires the Workspace to the Connection catalog and `sessionCommand`, which runs an action tab as `sshctl <action> -- <alias>`. With no arguments sshctl opens the Workspace only when stdin and stdout are both terminals, and prints help otherwise.
- `internal/catalog`: the Connection catalog. Listing Connections (`Snapshot`, with `Entry` and `HasGroup`), saving, deleting, regrouping them, reading and setting the Selected identity, recording an open in History, saving collapsed Groups and the dotfiles settings (global and per Group; `ProvisionRepo` picks the one that applies) go through it, so the rename, drop and write-order rules live only here. `groups.go` holds the Group operations (add, rename, delete, move), each within one Space; `spaces.go` the Space operations (add, rename, delete, move, putting a Connection in a Space, the active Space). A Space is passed by name, and "" names the default Space.
- `internal/config`: the versioned JSON config, split per ADR-0004: `config.json` (Spaces, Groups, Connection metadata, Provision, `workspace` preferences; synced by chezmoi) and `state.json` beside it (`config.State`: History, Selected identity and keys, collapsed Groups, list width, active Space; this machine only). Groups and Connections refer to the default Space as "" (ADR-0005), so renaming it rewrites only its record. `Load`/`Save` read and write both; migration from single-file and legacy configs. Both files live in `~/.config/sshctl/` on every OS (or `$XDG_CONFIG_HOME/sshctl/`), resolved by `Paths`; `MoveOld`, called before `Load`, moves them once from the old macOS/Windows folder.
- `internal/atomicfile`: the one atomic file write, through symlinks, that config and openssh share.
- `internal/openssh`: everything that runs or reads OpenSSH: ssh_config aliases (following `Include`), `ssh` argv (`ConnectArgs`), Host block writes to `~/.ssh/sshctl_config` and the Include (`managed.go`), scripts run over ssh for Bootstrap and Provision (`remote.go`), key generation (`keygen.go`), `ssh-add`, `ssh-keygen -R`.
- `internal/platform`: opens a new OS terminal window running `ssh` for `group connect`.
- `internal/workspace`: the Workspace TUI (Bubble Tea v2). `state.go` is the pure State; `model.go` the Bubble Tea Model; `keys.go` the key bindings, declared once with their context and help text, which drive both key handling and the help screen; `view.go` rendering; `term.go` the only importer of `charmbracelet/x/vt`; `sessions.go` the Session lifecycle (open, end, reconnect, close; the only writer of a Session's live/ended fields, owner of its runner); `runner.go` a Session's process in a pseudo-terminal (go-pty, ConPTY on Windows); `groups.go` the Group header actions (add, rename, delete); `form.go` the add/edit connection form; `modal.go` the modals (form, prompts, confirmation, help screen).

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
- **Recording fake Command** (`fixture_test.go`): `newRecordingModel(t, entries, fail...)` returns a Workspace and a `recorder` of every `Command` call (alias, Launch) and every alias its `fakeCatalog` recorded as opened. Its Command runs the test binary with no tests, which exits at once on every platform, and fails for the aliases in `fail`. Use it for tests about which Sessions start, not what they print.
- **OpenSSH** (`internal/openssh`): tests write temporary ssh_config trees.
- **Connection catalog** (`internal/catalog`): tests use a temporary sshctl config and ssh_config tree (`newTree`). Workspace tests replace it with `fakeCatalog` (`fixture_test.go`) through `Options.Catalog`, which every Workspace needs.

Tests never start the real `ssh`. `Options.Command` and `Options.Catalog` are the seams for replacing it and the config.

## Commit & Pull Request Guidelines

Use concise imperative commit subjects (for example, `Add SSH config parsing`). Pull requests should explain the change and rationale, list the validation commands and results, and link the issue. Include screenshots only when a change adds a visual interface.

Ticket branches start from `main` and open a pull request back into it. Work spanning several tickets gets a feature branch off `main` (as `connection-bootstrap` was for #36); its ticket branches start from it and merge back into it, and the feature branch then goes to `main` as one pull request. Before handing off, merge the base branch's current tip into the ticket branch and re-run the checks.

## Security & Configuration

Do not commit credentials, private keys, or machine-specific configuration. Keep secrets in environment variables or an approved secret store, and document required configuration with safe example values.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `jackykornet/sshctl`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default triage labels are used as-is (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
