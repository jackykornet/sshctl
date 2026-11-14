package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/openssh"
)

const bootstrapUsage = "bootstrap <alias>"

func (a *app) bootstrapCommand(args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: sshctl " + bootstrapUsage)
	}
	return a.bootstrap(args[0])
}

// bootstrap makes alias reachable with the Selected identity (see Bootstrap
// in GLOSSARY.md), in four steps:
//
//  1. For a Managed connection, write HostName, User and Port into its Host
//     block, as ssh resolves them now, so they no longer depend on defaults.
//  2. Install the public key on the host over plain ssh, which asks for
//     the password; a key already there is not added again.
//  3. Prove that the key alone logs in.
//  4. Only then set the Host block's IdentityFile to the key.
//
// If a step fails, the later ones do not run. A Connection that is not
// managed only gets the key: its Host block is the user's (ADR-0002). It
// runs in the user's terminal, so ssh can ask for the password there.
func (a *app) bootstrap(alias string) error {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	i := -1
	for j, e := range snap.Entries {
		if e.Alias == alias && !e.Orphaned {
			i = j
		}
	}
	if i < 0 {
		return fmt.Errorf("%s is not a Host in ssh_config", alias)
	}
	e := snap.Entries[i]
	key := snap.Identity
	if key == "" {
		return errors.New("no Selected identity to install; choose one with: sshctl key use <private-key-path>")
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return fmt.Errorf("the Selected identity's public key: %w", err)
	}
	shown := openssh.HomeRelative(key, a.home())
	ctx := context.Background()
	fmt.Fprintf(a.out, "Bootstrapping %s with %s\n", alias, shown)

	if e.Managed {
		if err := a.completeEndpoint(ctx, e); err != nil {
			return err
		}
	}

	fmt.Fprintln(a.out, "Installing the key on the host; ssh may ask for your password.")
	added, err := openssh.InstallKey(ctx, a.runSSH, alias, key, string(pub))
	switch {
	case errors.Is(err, openssh.ErrNoPOSIX):
		return fmt.Errorf("cannot bootstrap %s: %w", alias, err)
	case err != nil:
		return fmt.Errorf("installing the key on %s: %w", alias, err)
	case added:
		fmt.Fprintln(a.out, "Key added to ~/.ssh/authorized_keys on the host.")
	default:
		fmt.Fprintln(a.out, "Key already in ~/.ssh/authorized_keys on the host.")
	}

	fmt.Fprintln(a.out, "Checking that the key alone logs in.")
	if err := openssh.CheckKeyOnly(ctx, a.runSSH, alias, key); err != nil {
		left := "Its Host block is unchanged."
		if e.Managed {
			left = "IdentityFile was not set in its Host block."
		}
		return fmt.Errorf("%s did not accept %s without a password (%v). %s If the key has a passphrase, load it first with: sshctl agent ensure", alias, shown, err, left)
	}

	if !e.Managed {
		fmt.Fprintf(a.out, "Done. %s's Host block is not managed by sshctl, so it was left as it is; add IdentityFile %s to it yourself if you like.\n", alias, shown)
		return nil
	}
	snap, err = a.catalog().Snapshot()
	if err != nil {
		return err
	}
	for _, now := range snap.Entries {
		if now.Alias == alias {
			e = now
		}
	}
	e.Host.IdentityFile = key
	if err := a.saveEndpoint(e); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Done. Set IdentityFile %s in %s's Host block.\n", shown, alias)
	return nil
}

// completeEndpoint is Bootstrap's first step: it fills the HostName, User
// and Port that e's Host block lacks with what ssh resolves for it now.
func (a *app) completeEndpoint(ctx context.Context, e catalog.Entry) error {
	resolved, err := openssh.Resolve(ctx, a.runSSH, e.Alias)
	if err != nil {
		return err
	}
	var set []string
	for _, f := range []struct {
		name      string
		have      *string
		resolvedv string
	}{
		{"HostName", &e.Host.HostName, resolved.HostName},
		{"User", &e.Host.User, resolved.User},
		{"Port", &e.Host.Port, resolved.Port},
	} {
		if *f.have == "" && f.resolvedv != "" {
			*f.have = f.resolvedv
			set = append(set, f.name+" "+f.resolvedv)
		}
	}
	if len(set) == 0 {
		fmt.Fprintf(a.out, "%s's Host block already has HostName, User and Port.\n", e.Alias)
		return nil
	}
	if err := a.saveEndpoint(e); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set %s in %s's Host block.\n", strings.Join(set, ", "), e.Alias)
	return nil
}

// saveEndpoint writes e's Host block, keeping its description and group.
// SaveHost writes an IdentityFile under the home directory as ~/ (ADR-0004).
func (a *app) saveEndpoint(e catalog.Entry) error {
	return a.catalog().Save(e.Alias, catalog.Spec{Alias: e.Alias, Description: e.Description, Group: e.Group, Managed: true, Host: e.Host})
}

// home is the user's home directory, as SaveHost sees it.
func (a *app) home() string {
	if a.ssh.Home != "" {
		return a.ssh.Home
	}
	h, _ := os.UserHomeDir()
	return h
}
