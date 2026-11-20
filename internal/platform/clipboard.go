package platform

import (
	"errors"
	"os"
)

// ReadClipboard returns the text on the system clipboard of the machine
// sshctl runs on. It reads it only on Windows and macOS, and only when
// sshctl is not itself running over ssh, where that clipboard is not the
// user's; otherwise it returns errors.ErrUnsupported. An empty clipboard, or
// one holding no text, gives "".
func ReadClipboard() (string, error) {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
		return "", errors.ErrUnsupported
	}
	return readClipboard()
}
