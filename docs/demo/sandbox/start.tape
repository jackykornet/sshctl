# Shared by every docs/demo/*.tape (through Source): the terminal settings,
# then, hidden, setup.sh and sshctl, so each recording starts on the
# Workspace's connection list in Space 1.

Set Shell bash
Set FontSize 14
Set Width 1100
Set Height 560
Set Padding 12
Set Framerate 20
Set TypingSpeed 90ms
Set Theme "Catppuccin Mocha"

Hide
Type "source docs/demo/sandbox/setup.sh && clear" Enter
Sleep 4s
Type "sshctl" Enter
Sleep 1s
Show
