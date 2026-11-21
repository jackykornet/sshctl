---
status: accepted
---

# One rule for Workspace keys, and hints built from the bindings

The Workspace's keys grew one feature at a time. Previous and next tab had three spellings (`Alt+←/→` anywhere, `[`/`]` in the list, prefix `[`/`]`); `Alt+↑/↓` reached past the pane to switch Connections, so a Session never saw those keys; `r` reconnected an ended tab while prefix `r` refreshed the list; and every hint line was worded by hand, so a hint could name a key that was not bound where it showed.

Every Workspace key follows one rule:

1. **List actions work bare while the list has focus, and after the prefix from anywhere.** A list action is a command on the list's Connections, Groups or Spaces, or on tabs: filter, add/edit/delete, the Space keys (`P`, `S`, `1`–`9`), previous/next tab (`[`/`]`), previous/next open connection (`{`/`}`), quit. Keys that only move around the list (cursor, collapse, Group order, Enter, Esc clearing the filter) stay bare in the list.
2. **The pane takes only prefixed Workspace commands; every other key goes to the Session.** An ended tab has no Session to take keys, so it keeps its single-key actions (`r` reconnect, `x` close).
3. **A key means the same thing in every context.** For example refresh is prefix `R`, so `r` is only reconnect.

A modal (the form, prompts, confirmations, the Space picker, the help screen) and scroll mode take keys before all of this and act on themselves; their own keys keep the meanings the list gives them where they overlap (`a`/`e`/`d` add, rename, delete; `Shift+↑/↓` or `K`/`J` move; Esc leaves).

Every hint line uses one format, `key action · key action`, the prefix shown as `^B` (or the configured key), the most important action first. One helper (`hintsFor` in `internal/workspace/hints.go`) builds each line from the key table of the place it shows: `bindings` for the status line and the ended banner, and each modal's own table. A test checks that every key a hint names is bound there, to the action it names.

## Considered options

- **Keep the overlaps and document them:** the help screen would have kept three ways to change tabs, and new users would still meet keys that differ between the list and the pane.
- **Bare keys in the pane while a Session is live (Alt+arrows):** programs in the Session (shells, editors, tmux) use Alt+arrows themselves, and they never received them.
- **Hand-written hints with a test that parses them:** the wording would still drift from the bindings; building the line from the table makes an unbound key impossible to name.

## Consequences

- Removed: `Alt+←/→` (use `[`/`]`, or prefix `[`/`]` in the pane) and `Alt+↑/↓` (now `{`/`}`, bare in the list or after the prefix). In the pane these Alt keys now reach the Session.
- Renamed: refresh from prefix `r` to prefix `R`.
- Added, by rule 1: prefix `P` (Space picker), prefix `/` (filter, moving focus to the list), prefix `Ctrl+C` (quit).
- Hint wording changed: confirmations end in `y yes · n no` instead of `(y/n)`; the form says `^S save · Tab/Shift+Tab move · Esc cancel`; a modal that swallowed a prefix command says `Esc cancel` (or `Esc close`) instead of `… open: Esc to cancel`.
- A new key is added to a table (`bindings` or the modal's `keys`) with a name if it is hinted; the help screen and hints follow from it.
