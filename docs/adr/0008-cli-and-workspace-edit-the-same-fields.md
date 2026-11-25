---
status: accepted
---

# The CLI and the Workspace edit the same Connection fields, the same way

The Workspace's add/edit form set Alias, HostName, User, Port, Identity file, Description, Space and Group, and wrote the Host block to `~/.ssh/sshctl_config` (ADR-0002). `sshctl add` and `sshctl edit` only prompted for a description and a group, so a Connection could not be created from the command line, and the two front ends drifted apart in what they validated and how they named Groups. Now both edit the same fields through the same catalog calls:

- `sshctl add` and `sshctl edit` take every field the form has as a flag (`--host`, `--user`, `--port`, `--identity`, `--description`, `--space`, `--group`, and `--alias` on `edit` to rename). Without flags they prompt for each field, and an empty answer keeps the current value.
- Both save through `catalog.Save` with a `catalog.Spec`, as the form does, so validation, the refusal of an existing alias, the offer to add the Include and the write order (sshctl config read before the Host block is written, Host block put back if the config cannot be saved) live only in the catalog.
- Both look up a Space and a Group with the catalog's one resolver (`ResolveSpace`, `ResolveGroup`): a name or a number, `0` for ungrouped. Changing a Connection's Space without naming a Group leaves it ungrouped in both.
- For a Connection whose Host block is the user's (defined outside `sshctl_config`), both change only its sshctl metadata (description, Space, group) and say so; the CLI refuses Host field flags for it.
- `add` creates a new Connection, as the form's `a` does, and refuses an alias that exists; adding metadata to a Connection the user defined is `edit`, as it is `e` in the Workspace.
- Removing is the same too. `sshctl remove` used to delete only the metadata and keep even a Host block sshctl wrote, while the Workspace's `d` deleted that Host block as well. Both now call `catalog.Delete`: the Host block goes when sshctl wrote it, a Host block elsewhere in ssh_config stays, and the metadata goes. Both ask first; the CLI's `--yes` skips the question.

A field added to Connections later is added to the form and to both commands together, and Space and Group lookups go through the resolver rather than each front end's own parsing. Tunnels (ADR-0012) are the one exception on the command line: `sshctl tunnel` adds and removes them, and `add` and `edit` keep a Connection's Tunnels as they are.

## Considered options

- **Keep the CLI to metadata:** the CLI stays small, but a host can only be created in the Workspace or by editing ssh_config, and every rule about Groups and Spaces is written twice.
- **A separate `sshctl host` command for Host blocks:** splits one Connection across two commands, where the Workspace edits it in one form.
- **Keep `remove` metadata-only and give it a flag to delete the Host block:** the same word would mean less on the command line than in the Workspace.

## Consequences

- Breaking for scripts: `add` on an alias that is already in ssh_config is refused (use `edit`), and `add` on a new alias writes a Host block, so it needs a HostName; `edit` no longer needs existing metadata; `remove` deletes a Host block sshctl wrote and asks first unless given `--yes`.
- `catalog.DeleteMetadata`, used only by the old `remove`, is gone.
