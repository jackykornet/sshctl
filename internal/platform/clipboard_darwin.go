package platform

import "os/exec"

func readClipboard() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	return string(out), err
}
