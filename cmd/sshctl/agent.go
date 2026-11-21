package main

import (
	"fmt"

	"github.com/sshctl/sshctl/internal/openssh"
)

func (a *app) agentEnsure() error {
	status, err := openssh.AgentStatus()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, "SSH agent:", status)
	key, err := a.identity()
	if err != nil {
		return err
	}
	if key == "" {
		fmt.Fprintln(a.out, "No Selected identity; OpenSSH config identities remain in effect.")
		return nil
	}
	loaded, checkable, err := openssh.KeyLoaded(key)
	if err != nil {
		return err
	}
	if loaded {
		fmt.Fprintln(a.out, "Selected key is already loaded.")
		return nil
	}
	if !checkable {
		fmt.Fprintln(a.out, "No adjacent .pub file; loaded status cannot be checked without unlocking the private key.")
	}
	fmt.Fprintf(a.out, "Add the Selected identity %q to the agent? [y/N] ", key)
	if !confirm(a.in) {
		return nil
	}
	return openssh.AddKey(key)
}
