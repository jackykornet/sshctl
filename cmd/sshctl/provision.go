package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

const provisionUsage = "provision <alias> | provision default [<url|none> | --unset] [--group <name>]"

func (a *app) provisionCommand(args []string) error {
	if len(args) > 0 && args[0] == "default" {
		return a.provisionDefault(args[1:])
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: sshctl " + provisionUsage)
	}
	return a.provision(args[0])
}

// provision applies the dotfiles repo to alias's host with chezmoi. It fails
// before changing anything when no repo applies or the host has no POSIX sh;
// after that, a failure is the script's, with its exit code.
func (a *app) provision(alias string) error {
	repo, err := a.catalog().ProvisionRepo(alias)
	switch {
	case errors.Is(err, catalog.ErrNoDotfiles):
		return fmt.Errorf("%w; set one with: sshctl provision default <url>", err)
	case err != nil:
		return err
	}
	ctx := context.Background()
	key := a.cfg.DefaultKey
	if err := openssh.CheckPOSIX(ctx, a.runSSH, alias, key); err != nil {
		if errors.Is(err, openssh.ErrNoPOSIX) {
			return fmt.Errorf("cannot provision %s: %w", alias, err)
		}
		return err
	}
	fmt.Fprintf(a.out, "Provisioning %s with %s\n", alias, repo)
	return a.runSSH(ctx, openssh.RemoteArgs(alias, key, provisionScript(repo)), nil)
}

const provisionDefaultUsage = "usage: sshctl provision default [<url|none> | --unset] [--group <name>]"

// provisionDefault lists the dotfiles settings, or sets or unsets the
// global one or, with --group, a Group's.
func (a *app) provisionDefault(args []string) error {
	if len(args) == 0 {
		return a.listDotfiles()
	}
	var repo, group string
	unset, grouped := false, false
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--unset" && !unset:
			unset = true
		case arg == "--group" && !grouped && i+1 < len(args) && args[i+1] != "":
			grouped = true
			i++
			group = args[i]
		case arg != "" && !strings.HasPrefix(arg, "-") && repo == "":
			repo = arg
		default:
			return errors.New(provisionDefaultUsage)
		}
	}
	if unset == (repo != "") {
		return errors.New(provisionDefaultUsage)
	}
	if grouped {
		return a.catalog().SetGroupDotfiles(group, repo)
	}
	return a.catalog().SetDotfiles(repo)
}

// listDotfiles prints the global dotfiles setting, then each Group's in
// the user's order.
func (a *app) listDotfiles() error {
	cat := a.catalog()
	repo, err := cat.Dotfiles()
	if err != nil {
		return err
	}
	snap, err := cat.Snapshot()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, "global:", describeDotfiles(repo))
	for _, g := range snap.Groups {
		fmt.Fprintf(a.out, "group %s: %s\n", g.Name, describeDotfiles(g.Dotfiles))
	}
	return nil
}

func describeDotfiles(repo string) string {
	switch repo {
	case "":
		return "unset"
	case config.DotfilesNone:
		return "none (Provision refused)"
	}
	return repo
}

// provisionScript is the POSIX sh script Provision runs on the host. It runs
// only chezmoi, never sudo, and writes only inside the remote home: an
// installed chezmoi (on PATH or in ~/.local/bin, which a non-interactive
// shell may not have on PATH) updates and applies the dotfiles; otherwise
// chezmoi's install script, fetched with curl or else wget, installs it in
// ~/.local/bin and applies repo. The exit code is chezmoi's.
func provisionScript(repo string) string {
	return `bin="$HOME/.local/bin"
if command -v chezmoi >/dev/null 2>&1; then exec chezmoi update --apply; fi
if [ -x "$bin/chezmoi" ]; then exec "$bin/chezmoi" update --apply; fi
if command -v curl >/dev/null 2>&1; then installer=$(curl -fsLS get.chezmoi.io) || exit
elif command -v wget >/dev/null 2>&1; then installer=$(wget -qO- get.chezmoi.io) || exit
else echo "sshctl: Provision needs curl or wget on the host to install chezmoi" >&2; exit 1
fi
exec sh -c "$installer" -- -b "$bin" init --apply ` + openssh.ShellQuote(repo) + "\n"
}
