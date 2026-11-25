---
status: accepted
---

# Indicator colors are synced metadata on Groups and Spaces

Users keep production and test hosts side by side, and a shell prompt on one looks like a shell prompt on the other. An **Indicator color** warns them which kind of host they are typing into: a Group or a Space can carry one, such as red for production, and the Workspace draws the Active connection's color around its pane.

- The colors are basic ANSI names only: `red`, `yellow`, `green`, `cyan`, `blue`, `magenta`, or none (the default). They follow the user's terminal theme, as the rest of the Workspace does.
- A color is stored on the Group and Space records in `config.json` (`"color": "red"`), never in `state.json`, so chezmoi syncs it with the Groups and Spaces it belongs to (ADR-0004). It survives renaming and moving them, and goes when they are deleted.
- It resolves Group → Space → none, through the Connection catalog next to `ProvisionRepo`: a Group's own color wins over its Space's, and an ungrouped Connection takes its Space's.
- The Workspace draws the Active connection's color on the pane frame: the divider between the list and the pane, the rules above and below them, the alias in the pane header, the current tab and the `group / alias` breadcrumb in the status bar. Each Group header and each Space in the Space bar and picker is drawn in its own resolved color. Connection rows keep their usual colours, and the row dot still shows Session state. With no color set the Workspace looks as it did before.
- On the current tab the color takes the accent colour's place in the focus cue: bold reverse video in the color while the pane has focus, plain reverse video in the color while it has not (the tab line drew the current tab as coloured text until #150 made every current tab a block). The `LIST`/`SESSION` tag and the list's cursor row keep the cyan accent, so focus is always shown the same way.
- The CLI (`sshctl group color`, `sshctl space color`) and the Workspace (`C`, and `C` in the Space picker) set the same fields (ADR-0008); `C` follows the key rule of ADR-0007.

## Considered options

- **Hex or 256-colour values:** they ignore the terminal's theme and may be unreadable on it, and need validation and a picker. Six named colours are enough to tell a few kinds of host apart.
- **A color per Connection:** the kinds of host the user wants to tell apart are already Groups or Spaces (production, a customer), and one setting per Group keeps every member in step; a per-Connection color would also need its own place in the form and the CLI.
- **An automatic palette (a color per Space or Group by position):** colors that carry no meaning the user chose would dilute the warning. Only a color the user set is drawn.
- **Coloring the whole pane or every row:** the frame is enough to be seen without making the Session's own colours harder to read.

## Consequences

- The config format becomes Version 7. An older sshctl refuses a Version 7 file ("unsupported config version 7") rather than save it without the colors. A machine syncing the config through chezmoi needs the newer sshctl too. Load migrates an older config without any colors.
- A color sshctl does not know, from a newer version or a hand edit, is kept in the file and drawn as none, and Load reports it with the valid names.
