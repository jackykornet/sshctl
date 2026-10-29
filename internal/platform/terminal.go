package platform

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func OpenSSH(alias, key string) error {
	sshArgs := []string{}
	if key != "" {
		sshArgs = append(sshArgs, "-i", key)
	}
	sshArgs = append(sshArgs, alias)
	var name string
	var args []string
	switch runtime.GOOS {
	case "windows":
		if _, err := exec.LookPath("wt.exe"); err == nil {
			name = "wt.exe"
			args = append([]string{"new-tab", "ssh"}, sshArgs...)
		} else {
			name = "cmd"
			args = append([]string{"/c", "start", "ssh"}, sshArgs...)
		}
	case "darwin":
		name = "open"
		args = append([]string{"-a", "Terminal", "--args", "ssh"}, sshArgs...)
	default:
		for _, candidate := range []string{"gnome-terminal", "konsole", "xfce4-terminal", "x-terminal-emulator"} {
			if _, err := exec.LookPath(candidate); err == nil {
				name = candidate
				break
			}
		}
		if name == "" {
			return fmt.Errorf("no supported terminal found (install gnome-terminal, konsole, xfce4-terminal, or x-terminal-emulator)")
		}
		switch name {
		case "gnome-terminal":
			args = append([]string{"--", "ssh"}, sshArgs...)
		case "konsole":
			args = append([]string{"-e", "ssh"}, sshArgs...)
		default:
			args = append([]string{"-e", "ssh"}, sshArgs...)
		}
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
