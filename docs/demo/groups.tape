# Groups, in docs/guide.md's Groups section. See workspace.tape for how to
# re-record.

Output docs/demo/groups.gif
Source docs/demo/sandbox/start.tape

Sleep 800ms

# Collapse and expand prod (the cursor starts on its header).
Enter
Sleep 900ms
Enter
Sleep 900ms

# Move staging above prod.
Down 4 Sleep 400ms
Type "K"
Sleep 1.2s

# Move bastion into staging.
Down 8 Sleep 400ms
Ctrl+B Type "m"
Sleep 600ms
Type "staging" Sleep 300ms Enter
Sleep 1.5s

# Filter by group name.
Type "/prod"
Sleep 1.5s
Escape
Sleep 1s

Hide
Ctrl+B Type "q"
Sleep 500ms
Type "y"
