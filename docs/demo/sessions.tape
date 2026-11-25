# Sessions and tabs, in docs/guide.md's Workspace section. See
# workspace.tape for how to re-record.

Output docs/demo/sessions.gif
Source docs/demo/sandbox/start.tape

Sleep 800ms

# Open db-main into a tab.
Down Sleep 400ms
Enter
Sleep 1s
Type "uptime" Sleep 300ms Enter
Sleep 1s

# The tab is named after the title the shell sets: its directory.
Type "cd /var/log" Sleep 300ms Enter
Sleep 1.2s

# A second tab on the same connection.
Ctrl+B Type "n"
Sleep 1s
Type "ls" Sleep 300ms Enter
Sleep 1s

# Name the tab yourself: the prompt starts with its automatic name, ~.
Ctrl+B Type ","
Sleep 500ms
Backspace Sleep 200ms
Type "deploy" Sleep 300ms Enter
Sleep 1.2s

# Back to the first tab.
Ctrl+B Type "["
Sleep 1.2s

# Open web-1 as well; db-main stays open, marked in the list.
Ctrl+B Tab
Sleep 300ms
Down Sleep 400ms
Enter
Sleep 1.2s

# The session ends; its tab stays with the exit code, and r reconnects.
Type "exit" Sleep 300ms Enter
Sleep 1.5s
Type "r"
Sleep 1.5s

# Switch back to db-main, its tabs as they were.
Ctrl+B Tab
Sleep 300ms
Up Sleep 400ms
Enter
Sleep 1.5s

Hide
Ctrl+B Type "q"
Sleep 500ms
Type "y"
