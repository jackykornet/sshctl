# The Workspace GIF in README.md. Re-record from the repository root with
#   vhs docs/demo/workspace.tape
# setup.sh runs sshctl against the made-up hosts in docs/demo/home, with
# docs/demo/bin/ssh in place of OpenSSH.

Output docs/demo/workspace.gif

Set Shell bash
Set FontSize 14
Set Width 1100
Set Height 560
Set Padding 12
Set Framerate 20
Set TypingSpeed 90ms
Set Theme "Catppuccin Mocha"

Hide
Type "source docs/demo/setup.sh && clear" Enter
Sleep 4s
Type "sshctl" Enter
Sleep 1s
Show

# The connection list: Groups in Space 1, the Space bar above.
Sleep 1.5s

# Open db-main (the list is sorted) into a tab and run a command there.
Down Sleep 400ms
Enter
Sleep 1s
Type "uptime" Sleep 300ms Enter
Sleep 1.5s

# Back to the list, filter it.
Ctrl+B Tab
Sleep 500ms
Type "/db"
Sleep 1.5s
Escape
Sleep 700ms

# Switch to Space 2 and open a connection there.
Type "2"
Sleep 1.2s
Down Sleep 400ms
Enter
Sleep 1.5s

# Back to Space 1, where db-main is still open.
Ctrl+B Type "1"
Sleep 2s

Hide
Ctrl+B Type "q"
Sleep 500ms
Type "y"
