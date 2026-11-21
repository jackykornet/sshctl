---
status: accepted
---

# Space numbers are fixed, up to nine

The Workspace switches to Spaces 1-9 by their number, so users learn "customer1 is 4". A Space's number was its position in `config.json`'s ordered `spaces` list (ADR-0005), so deleting Space 3 turned 4 into 3 and broke what the user had learned. sshctl now stores each Space's number in its record (`"number": 4`). A Space keeps its number until the user changes it: deleting a Space leaves its number free, and no other Space renumbers.

- There are at most nine Spaces, one per number key. A new Space takes the lowest free number unless the user picks one, and a tenth is refused.
- Moving a Space to a number another Space has swaps the two. Moving it to a free number just moves it.
- The default Space has a number like any other: 1 in a new config, and it can be moved.
- The `spaces` list stays in `config.json`, sorted by number, so readers that walk it see Spaces in number order.

## Considered options

- **Keep positions and only stop renumbering on delete (leave a hole in the list):** a hole in a JSON array has no natural form, and moving a Space would still shift the others.
- **More than nine Spaces, the rest reached only through the picker or filter:** a Space without a number key loses what the feature is for. Nine fits the keys and has been enough so far.

## Consequences

- The config format stays Version 4. Load numbers a config without numbers once, 1 to N in its order. A Space whose number an earlier Space already has, or one out of range (from a hand edit or a sync merge), takes the lowest free number.
- A config with ten or more Spaces still loads: the first nine get numbers and the rest have none. sshctl never drops a Space or refuses to start. A Space without a number can be given a free one, but cannot swap with a numbered Space, which would leave that Space without one.
- An older sshctl ignores the `number` field, but drops it when it saves. The next newer sshctl then numbers the Spaces again in their order.
- Group numbers are unchanged: they are still positions and shift when a Group is deleted (ADR-0003).
