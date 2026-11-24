---
status: accepted
---

# Tunnels are Connection metadata, run as ssh -N -L

The user wants to reach a service the server does not expose publicly, such as an admin web UI on port 8083, through an SSH local forward (`ssh -L 8083:localhost:8083 <alias>`), without typing the forward each time. A **Tunnel** is such a forward saved on a Connection. Tunnels are stored as Connection metadata in `config.json` (`connections.<alias>.tunnels`, each `{name?, local_port, remote_host, remote_port}`), so they sync with chezmoi (ADR-0004) like a description or group. sshctl runs a Connection's Tunnels as one `ssh -N -o ExitOnForwardFailure=yes -o ServerAliveInterval=30 -o ServerAliveCountMax=3 [-i key] -L … -L … <alias>`, and never changes the Host block. A run in the background (the Workspace) adds `-o BatchMode=yes`, since nothing can answer a prompt; `sshctl tunnel <alias>` runs in the user's terminal and may prompt. The config goes to Version 6, so an older sshctl refuses the file rather than save it without the Tunnels.

## Considered options

- **`LocalForward` lines in the Host block:** plain `ssh <alias>` would forward too, with nothing for sshctl to run. But every login Session to that Connection would then try to bind the same local ports, so a second tab would warn or, with `ExitOnForwardFailure`, fail. And sshctl only writes Managed connections' Host blocks (ADR-0002), so a Connection whose Host block is the user's could have no Tunnels.
- **Remote (`-R`) and dynamic (`-D`) forwards as well:** left for later. Local forwards cover reaching a service on or behind the server, which is what was asked for, and keep the spec syntax and the form to one shape.

## Consequences

- Tunnels follow the metadata rules: renaming a Connection carries them, an Orphaned connection keeps them, and a Connection whose only metadata is its Tunnels still has a record.
- A running Tunnel is not a Session: it gets no tab, does not record History and does not make the Connection an Open connection.
- `ExitOnForwardFailure` makes a Tunnel whose local port is taken fail at once rather than run without the forward, and `ServerAliveInterval`/`ServerAliveCountMax` make one whose server stops answering exit within about 90 seconds. A Tunnel that stops is restarted by hand, never automatically.
- Passwords are not stored; a server that still asks for one is set up with Bootstrap.
