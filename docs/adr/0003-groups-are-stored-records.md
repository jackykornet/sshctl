---
status: accepted
---

# Groups are stored records, not labels derived from Connections

Until now a Group existed only as a name in Connections' metadata. It vanished with its last member, was listed alphabetically, and kept its collapsed state in a separate list. A Group now also carries settings (its dotfiles repo for Provision, or **none** to rule Provision out for its Connections), and the user wants the connection list to look the way they set it up. So sshctl's config stores Groups as an ordered list of records. Each record holds the Group's name and its dotfiles repo, and a Connection's metadata names the Group it belongs to. Whether a Group is collapsed is not part of the record: it belongs to each machine (ADR-0004), keyed by Group name and kept in step when a Group is renamed or deleted. A Group lasts until the user deletes it. Deleting it leaves its Connections ungrouped, and renaming it moves every member.

## Considered options

- **Keep Groups as derived labels and key their settings by name:** smaller change, but a setting would outlive its Group and silently apply to a new Group that reuses the name. A stale **none** could block Provision without the user knowing why. Empty Groups and a chosen order would still be impossible.

## Consequences

- The config is migrated once: every Group name already used by a Connection becomes a Group record, in alphabetical order. Which Groups are collapsed is kept as it was.
- The order of Groups is the user's. Connections within a Group stay alphabetical.
