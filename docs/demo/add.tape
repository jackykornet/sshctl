# Adding a connection, in docs/guide.md's "Adding, editing and deleting
# connections" section. See workspace.tape for how to re-record.

Output docs/demo/add.gif
Source docs/demo/sandbox/start.tape

Sleep 800ms

# On a connection in prod, a opens the form with the group filled in.
Down Sleep 400ms
Type "a"
Sleep 800ms
Type "cache-1" Tab
Type "10.20.0.31" Tab
Type "redis" Tab
Tab
Tab
Type "redis cache"
Sleep 800ms
Ctrl+S
Sleep 1.2s

# The first save asks to add the Include to ~/.ssh/config.
Type "y"
Sleep 1.5s

# Open it.
Enter
Sleep 1.5s

Hide
Ctrl+B Type "q"
Sleep 500ms
Type "y"
