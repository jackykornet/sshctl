# Repository Guidelines

## Project Structure & Module Organization

This repository is currently a minimal Go development scaffold. The `.devcontainer/` directory defines the Go 1.25 development image and Node tooling; `.agents/skills/` contains repository workflow guidance, and `skills-lock.json` records the installed skill source. No Go module, application source, tests, or assets exist yet. As the project grows, keep Go code in the repository’s package directories and place tests beside the code they cover in `*_test.go` files.

## Build, Test, and Development Commands

Run commands from the repository root. Once a Go module is present, use:

- `go test ./...` to run all package tests.
- `go vet ./...` to check for common correctness issues.
- `gofmt -w <files.go>` to format changed Go files; the devcontainer also formats Go files on save.
- `go run .` to launch the application when a root `main` package exists.

There is no build or run target configured yet; add project-specific commands here when those are introduced.

## Coding Style & Naming Conventions

Follow standard Go conventions: tabs for indentation (as applied by `gofmt`), mixed-case exported identifiers, and short lowercase package names. Keep packages focused and avoid stuttering names (for example, `ssh.Client`, not `ssh.SSHClient`). Name tests after behavior or the unit under test, such as `TestParseConfig`.

## Testing Guidelines

Use Go’s built-in `testing` package and colocate tests with implementation. Prefer table-driven tests for multiple input cases. Run `go test ./...` before submitting changes; no coverage threshold or additional test framework is configured.

## Commit & Pull Request Guidelines

No Git commit history is available in this workspace, so no repository-specific commit convention can be inferred. Use concise imperative commit subjects (for example, `Add SSH config parsing`). Pull requests should explain the change and rationale, list relevant validation commands and results, and link any related issue. Include screenshots only when a change adds a visual interface.

## Security & Configuration

Do not commit credentials, private keys, or machine-specific configuration. Keep secrets in environment variables or an approved secret store, and document required configuration with safe example values.

## Agent skills

### Issue tracker

Issues and specs live in GitHub Issues for `jackykornet/sshctl`, managed with the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five default triage labels are used as-is (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
