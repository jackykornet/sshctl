package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sshctl/sshctl/internal/openssh"
)

// knownHostsList prints the host field of each known host, as stored.
func (a *app) knownHostsList(input) error {
	f, err := os.Open(openssh.KnownHostsPath())
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
}

// knownHostsRemove removes a known host with ssh-keygen -R, after asking.
func (a *app) knownHostsRemove(in input) error {
	args := in.args
	if len(args) != 1 {
		return errors.New("usage: sshctl known-hosts remove <host>")
	}
	fmt.Fprintf(a.out, "Remove known host %q? [y/N] ", args[0])
	if !confirm(a.in) {
		return nil
	}
	return openssh.RemoveKnownHost(args[0], openssh.KnownHostsPath())
}
