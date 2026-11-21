package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sshctl/sshctl/internal/openssh"
)

func (a *app) knownHosts(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshctl known-hosts list|remove <host>")
	}
	path := openssh.KnownHostsPath()
	switch args[0] {
	case "list":
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				parts := strings.Fields(line)
				if len(parts) > 0 {
					fmt.Fprintln(a.out, parts[0])
				}
			}
		}
		return sc.Err()
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: sshctl known-hosts remove <host>")
		}
		fmt.Fprintf(a.out, "Remove known host %q? [y/N] ", args[1])
		if !confirm(a.in) {
			return nil
		}
		return openssh.RemoveKnownHost(args[1], path)
	}
	return errors.New("usage: sshctl known-hosts list|remove <host>")
}
