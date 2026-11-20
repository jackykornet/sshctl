---
status: accepted
---

# The Workspace selects text in a Session's pane itself

The Workspace turns on mouse reporting (`MouseModeCellMotion`) so clicks, drag-to-group and the wheel work. A terminal that reports the mouse to a program stops selecting text and pasting on right-click itself, so in Windows Terminal and others the user could not copy from a Session or paste into it with the mouse. Shift+drag bypasses the capture, but then the terminal selects whole screen rows, across the Connection list and the pane together.

sshctl keeps capturing the mouse and selects in the pane itself, as tmux does. A left-drag in a Session's pane selects cells of that pane only and highlights them; release copies the text with OSC 52 (`tea.SetClipboard`). A right-click pastes into a live Session through bracketed paste, reading the clipboard from the OS on Windows and macOS when sshctl runs locally, and else asking the terminal with OSC 52. If neither gives text, the status line says to use Ctrl+Shift+V. While a full-screen program in a live Session is on the alternate screen, the mouse still goes to it, and the Connection list takes no part in selection.

## Considered options

- **Release the mouse to the terminal while the pointer is over the pane:** a terminal has no per-region mouse capture, so turning reporting off and on as the pointer moves would lose clicks and still select across the list.
- **A key that turns mouse capture off ("copy mode" in the host terminal):** the terminal's own selection would still span the list, and the user has to remember to turn it back on.

## Consequences

- Copying needs a terminal that accepts OSC 52 writes; Windows Terminal, iTerm2, kitty, WezTerm and recent xterm do.
- Reading the clipboard over OSC 52 is often refused, Windows Terminal included, so `internal/platform` reads the OS clipboard on Windows (Win32) and macOS (`pbpaste`), and sshctl does not read it when it runs over ssh itself.
- Selection works on what the pane shows, including history while scrolled back. vt does not record wrapped rows, so a wrapped line is copied as separate lines.
- The selection clears on the next key press or click, on the wheel, and when the Session's output changes.
