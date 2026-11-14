# sshctl

sshctl manages metadata about OpenSSH host aliases and gives a workspace for opening and switching between live SSH sessions, while OpenSSH itself does all SSH work.

## Connections and sessions

**Connection**:
One non-wildcard Host alias from the user's ssh_config, optionally carrying sshctl's own description and group. sshctl's metadata never defines how to reach the host; only a Host block does. Renaming a Connection carries its metadata and History to the new alias. A Connection with neither a description nor a group has no sshctl metadata.
_Avoid_: Server, host, profile

**Managed connection**:
A Connection whose Host block is in sshctl's own file, `~/.ssh/sshctl_config`, which the user's ssh_config Includes. The Workspace can add, edit, rename and delete it. Every other Connection's Host block belongs to the user, and sshctl only edits its description and group.
_Avoid_: sshctl host, owned connection

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

**Group**:
A named, ordered set of Connections that exists from when the user creates it until they delete it, even with no members. It keeps its place in the connection list and its dotfiles repo for Provision (or that Provision is ruled out). A Connection is in at most one group; one with none is **ungrouped**, and deleting a group leaves its Connections ungrouped.
_Avoid_: Folder, tag, category

**Group number**:
A Group's current position in the connection list, counting from 1, shown in its header; 0 stands for ungrouped. It changes when Groups are reordered or deleted, which is why a group name may not be only digits.
_Avoid_: Group ID, index

**History**:
The 20 most recently opened Connections, most recent first. A Connection is opened when a Workspace Session for it starts or when `sshctl connect` runs it.
_Avoid_: Recents, log

## Setting up a host

**Bootstrap**:
Making a Connection reachable with the user's own key: installing the Selected identity's public key on the host, proving that the key alone logs in, and then pointing a Managed connection's Host block at that key. With no Selected identity, it first generates a key and makes it the Selected identity. It never changes the host's SSH server settings and never includes Provision.
_Avoid_: Setup, onboard, copy-id

**Provision**:
Applying the user's dotfiles to a Connection's host, which in turn installs the tools the dotfiles ask for, without root and only inside the remote user's home. It happens only when the user chooses it for that Connection.
_Avoid_: Setup, personalize, deploy

**Harden**:
Changing a host's SSH server settings, such as turning off password login, so that the change undoes itself if the user would be locked out. It needs root and is never part of Bootstrap or Provision.
_Avoid_: Lock down, secure

## Workspace

**Workspace**:
The full-screen view shown by running `sshctl` without arguments: the connection list, the tabbed session pane and the status bar.
_Avoid_: UI, dashboard, window

## Identity

**Selected identity**:
The one private key sshctl passes to every `ssh` it starts, or none, in which case OpenSSH chooses keys itself.
_Avoid_: Default key, current key
