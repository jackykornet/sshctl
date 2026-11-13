---
status: accepted
---

# Workspace is a TUI that hosts `ssh` in pseudo-terminals with its own terminal emulator

The Workspace (`sshctl` with no arguments) is a full-screen TUI rather than a desktop GUI. This keeps sshctl a single cross-platform binary that still delegates all SSH work to the installed OpenSSH client. Each Session is a plain `ssh <alias>` process in a pseudo-terminal, and sshctl parses its output with an embedded terminal emulator so it can draw several Sessions side by side with the connection list. We use Bubble Tea v2 for the UI, `github.com/charmbracelet/x/vt` for terminal emulation and `github.com/aymanbagabas/go-pty` for pseudo-terminals, because go-pty supports Windows ConPTY as well as Unix.

## Considered options

- **Desktop GUI (Wails + xterm.js):** more polished and better mouse handling, but it needs a web frontend and per-OS packaging, and sshctl would stop being a CLI.
- **Native SSH via `golang.org/x/crypto/ssh` instead of spawning `ssh`:** this would avoid a pseudo-terminal on the local side, but sshctl would have to re-implement ssh_config, agents, ProxyJump and known_hosts, which OpenSSH already handles.
- **tcell/tview, `hinshun/vt10x`, `creack/pty`:** these are lower-level or not much maintained, and `creack/pty` is Unix only.

## Consequences

- A throwaway prototype (2026-11-13) ran `vim` and `htop` through go-pty and x/vt correctly on Linux (alternate screen, colors, status lines, wrapping at the pane width). Windows ConPTY is not yet verified.
- `x/vt` needs a newer `charmbracelet/x/ansi` than Bubble Tea v1 and Lip Gloss v1 can build with. Use Bubble Tea v2 and Lip Gloss v2 (`charm.land/...`), which share x/vt's rendering core.
- `x/vt` is still at v0.1.0, so expect API churn. Keep it behind a small internal interface so it can be swapped.
- The Workspace must forward key presses to the Session as terminal byte sequences itself, and send the emulator's replies (cursor position and device attribute reports) back to the pseudo-terminal.
