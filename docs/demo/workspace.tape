# The overview GIF at the top of README.md. Re-record every GIF from the
# repository root with
#   for t in docs/demo/*.tape; do vhs "$t"; done
# sandbox/start.tape runs sshctl against the made-up hosts in
# sandbox/home, with sandbox/bin/ssh in place of OpenSSH.

Output docs/demo/workspace.gif
Source docs/demo/sandbox/start.tape

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
