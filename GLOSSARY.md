# sshctl

sshctl manages metadata about OpenSSH host aliases and gives a workspace for opening and switching between live SSH sessions, while OpenSSH itself does all SSH work.

## Connections and sessions

**Connection**:
One non-wildcard Host alias from the user's ssh_config, optionally carrying sshctl's own description and group. It never defines how to reach the host.
_Avoid_: Server, host, profile

**Orphaned connection**:
A Connection whose sshctl description or group remains after its Host alias has disappeared from ssh_config. It cannot be opened.
_Avoid_: Stale, missing, saved-only connection

**Session**:
One `ssh` process for a Connection, shown as one tab. A Session is either **live** (the process is running) or **ended** (it has exited but the tab has not been closed). A Connection has zero or more Sessions.
_Avoid_: Tab (the tab is only its view), window, terminal

**Open connection**:
A Connection that has at least one Session, live or ended. It stops being open when its last tab is closed.
_Avoid_: Connected, active (see Active connection)

**Active connection**:
The single Connection currently selected in the workspace, whose Sessions are shown as tabs.
_Avoid_: Current, focused, selected connection

## Workspace

**Workspace**:
The full-screen view shown by running `sshctl` without arguments: the connection list, the tabbed session pane and the status bar.
_Avoid_: UI, dashboard, window

## Identity

**Selected identity**:
The one private key sshctl passes to every `ssh` it starts, or none, in which case OpenSSH chooses keys itself.
_Avoid_: Default key, current key
