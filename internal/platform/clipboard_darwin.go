package platform

import "os/exec"

// readClipboard runs pbpaste.
func readClipboard() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	return string(out), err
}
