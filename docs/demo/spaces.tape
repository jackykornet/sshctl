# Spaces, in docs/guide.md's Spaces section. See workspace.tape for how to
# re-record.

Output docs/demo/spaces.gif
Source docs/demo/sandbox/start.tape

Sleep 800ms

# Switch Space with its number, and back.
Type "2"
Sleep 1.5s
Type "1"
Sleep 1s

# The Space picker: add a Space on free number 3, then switch to it.
Type "P"
Sleep 1.2s
Down Sleep 300ms
Down Sleep 300ms
Type "a"
Sleep 600ms
Type "customer3" Sleep 300ms Enter
Sleep 1.2s
Enter
Sleep 1.5s

# Back to Space 2.
Type "2"
Sleep 1.5s

Hide
Ctrl+B Type "q"
Sleep 500ms
Type "y"
