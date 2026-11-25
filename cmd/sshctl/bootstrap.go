package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/openssh"
)

// bootstrapCommand runs `sshctl bootstrap <alias>`.
func (a *app) bootstrapCommand(in input) error {
	alias, ok := aliasArg(in)
	if !ok {
		return errUsage
	}
	return a.bootstrap(alias)
}

// aliasArg returns the one alias in, which may follow "--" so that an alias
// starting with "-" is not taken for an option.
func aliasArg(in input) (string, bool) {
	if len(in.args) == 1 && in.args[0] != "" {
		return in.args[0], true
	}
	return "", false
}

// bootstrap makes alias reachable with the Selected identity (see Bootstrap
// in GLOSSARY.md), in four steps. With no Selected identity it first
// generates a key and selects it (generateIdentity).
//
//  1. For a Managed connection, write HostName, User and Port into its Host
//     block, as ssh resolves them now, so they no longer depend on defaults,
//     and clear its IdentityFile, so an old key cannot pass step 3.
//  2. Install the public key on the host over plain ssh, which asks for
//     the password; a key already there is not added again.
//  3. Prove that the key alone logs in (ssh may ask for its passphrase),
//     noting any IdentityFile ssh_config still makes ssh offer.
//  4. Only then set the Host block's IdentityFile to the key.
//
// If a step fails, the later ones do not run, and an IdentityFile step 1
// cleared is put back. A Connection that is not managed only gets the key:
// its Host block is the user's (ADR-0002). It runs in the user's terminal,
// so ssh can ask for the password there.
func (a *app) bootstrap(alias string) error {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	e, ok := snap.Entry(alias)
	if !ok {
		return fmt.Errorf("%s is not a Host in ssh_config", alias)
	}
	if e.Orphaned {
		return fmt.Errorf("%s is missing from ssh_config", alias)
	}
	ctx := context.Background()
	key := snap.SpaceIdentity(e.Space)
	if key == "" {
		if key, err = a.generateIdentity(ctx); err != nil {
			return err
		}
	}
	pub, err := os.ReadFile(key + ".pub")
	if err != nil {
		return fmt.Errorf("the Selected identity's public key: %w", err)
	}
	shown := openssh.HomeRelative(key, a.home())
	fmt.Fprintf(a.out, "Bootstrapping %s with %s\n", alias, shown)

	if e.Managed {
		old := e.Host.IdentityFile
		if e, err = a.completeEndpoint(ctx, e); err != nil {
			return err
		}
		err := a.installAndCheck(ctx, e, key, string(pub))
		// Read the Connection again: its description or group may have
		// changed while ssh waited for a password.
		if now, rerr := a.catalog().Snapshot(); rerr == nil {
			if n, ok := now.Entry(alias); ok && n.Managed {
				e = n
			}
		}
		if err != nil {
			if old != "" {
				e.Host.IdentityFile = old
				if rerr := a.saveEndpoint(e); rerr != nil {
					return fmt.Errorf("%w (and putting back IdentityFile %s failed: %v)", err, old, rerr)
				}
				fmt.Fprintf(a.out, "Put back IdentityFile %s in %s's Host block.\n", old, alias)
			}
			return err
		}
		e.Host.IdentityFile = key
		if err := a.saveEndpoint(e); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Done. Set IdentityFile %s in %s's Host block.\n", shown, alias)
		return nil
	}
	if err := a.installAndCheck(ctx, e, key, string(pub)); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Done. %s's Host block is not managed by sshctl, so it was left as it is; add IdentityFile %s to it yourself if you like.\n", alias, shown)
	return nil
}

// installAndCheck is Bootstrap's second and third steps for e: install the
// public key pub of key on the host, then prove that key logs in on its own.
func (a *app) installAndCheck(ctx context.Context, e catalog.Entry, key, pub string) error {
	alias, shown := e.Alias, openssh.HomeRelative(key, a.home())

	fmt.Fprintln(a.out, "Installing the key on the host; ssh may ask for your password.")
	added, err := openssh.InstallKey(ctx, a.runSSH, alias, key, pub)
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

	fmt.Fprintln(a.out, "Checking that the key alone logs in; ssh may ask for its passphrase.")
	if err := openssh.CheckKeyOnly(ctx, a.runSSH, alias, key); err != nil {
		left := "Its Host block is unchanged."
		if e.Managed {
			left = "IdentityFile was not set in its Host block."
		}
		return fmt.Errorf("%s did not accept %s without a password (%v). %s", alias, shown, err, left)
	}
	// OpenSSH cannot be told to skip ssh_config's IdentityFiles, so say
	// when the check may have logged in with one of them.
	others, err := openssh.OtherIdentities(ctx, a.runSSH, alias, key, a.home())
	if err != nil {
		return err
	}
	if len(others) > 0 {
		fmt.Fprintf(a.out, "Note: ssh also offered %s from ssh_config, so the check does not prove that %s alone logs in.\n", strings.Join(others, ", "), shown)
	}
	return nil
}

// generateIdentity creates a new ed25519 key in ~/.ssh, never overwriting
// one, and makes it the global Selected identity, the one every Space
// without its own uses. ssh-keygen runs in the user's
// terminal, so it asks for a passphrase there.
func (a *app) generateIdentity(ctx context.Context) (string, error) {
	fmt.Fprintln(a.out, "There is no Selected identity, so sshctl creates an ed25519 key; ssh-keygen asks for a passphrase (leave it empty for none).")
	key, err := openssh.GenerateKey(ctx, a.runSSH, filepath.Join(a.home(), ".ssh"))
	if err != nil {
		return "", err
	}
	if err := a.catalog().SetIdentity(key); err != nil {
		return "", err
	}
	fmt.Fprintf(a.out, "%s is now the Selected identity.\n", openssh.HomeRelative(key, a.home()))
	return key, nil
}

// completeEndpoint is Bootstrap's first step: it fills the HostName, User
// and Port that e's Host block lacks with what ssh resolves for it now, and
// clears its IdentityFile. It returns e as saved.
func (a *app) completeEndpoint(ctx context.Context, e catalog.Entry) (catalog.Entry, error) {
	resolved, err := openssh.Resolve(ctx, a.runSSH, e.Alias)
	if err != nil {
		return e, err
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
	cleared := e.Host.IdentityFile
	e.Host.IdentityFile = ""
	if len(set) == 0 && cleared == "" {
		fmt.Fprintf(a.out, "%s's Host block already has HostName, User and Port.\n", e.Alias)
		return e, nil
	}
	if err := a.saveEndpoint(e); err != nil {
		return e, err
	}
	if len(set) == 0 {
		fmt.Fprintf(a.out, "%s's Host block already has HostName, User and Port.\n", e.Alias)
	} else {
		fmt.Fprintf(a.out, "Set %s in %s's Host block.\n", strings.Join(set, ", "), e.Alias)
	}
	if cleared != "" {
		fmt.Fprintf(a.out, "Cleared IdentityFile %s until the key is checked.\n", cleared)
	}
	return e, nil
}

// saveEndpoint writes e's Host block, keeping its description, group and
// Tunnels.
// SaveHost writes an IdentityFile under the home directory as ~/ (ADR-0004).
func (a *app) saveEndpoint(e catalog.Entry) error {
	s := catalog.SpecFor(e)
	s.Managed = true
	return a.catalog().Save(e.Alias, s)
}

// home is the user's home directory, as SaveHost sees it.
func (a *app) home() string {
	if a.ssh.Home != "" {
		return a.ssh.Home
	}
	h, _ := os.UserHomeDir()
	return h
}
