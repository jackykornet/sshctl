package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

// provisionCommand runs `sshctl provision <alias>`.
func (a *app) provisionCommand(in input) error {
	alias, ok := aliasArg(in)
	if !ok {
		return errUsage
	}
	return a.provision(alias)
}

// provision applies the dotfiles repo to alias's host with chezmoi. It fails
// before changing anything when no repo applies or the host has no POSIX sh;
// after that, a failure is the script's, with its exit code.
func (a *app) provision(alias string) error {
	repo, err := a.catalog().ProvisionRepo(alias)
	if errors.Is(err, catalog.ErrNoDotfiles) {
		repo, err = a.askDotfiles(alias, err)
	}
	if err != nil {
		return err
	}
	ctx := context.Background()
	key, err := a.identityFor(alias)
	if err != nil {
		return err
	}
	if err := openssh.CheckPOSIX(ctx, a.runSSH, alias, key); err != nil {
		if errors.Is(err, openssh.ErrNoPOSIX) {
			return fmt.Errorf("cannot provision %s: %w", alias, err)
		}
		return err
	}
	fmt.Fprintf(a.out, "Provisioning %s with %s\n", alias, repo)
	return a.runSSH(ctx, openssh.RemoteArgs(alias, key, provisionScript(repo)), nil)
}

// askDotfiles asks for the dotfiles repo when none applies to alias (noRepo
// is the catalog's ErrNoDotfiles), then whether to save it as the global
// setting, as the setting of alias's Space (offered when there are several
// Spaces), as the setting of alias's Group (offered only when it has one),
// or not at all. It reads a plain terminal, so it works in a Workspace tab
// too. Without an answer it returns noRepo with a hint and saves nothing.
func (a *app) askDotfiles(alias string, noRepo error) (string, error) {
	cat := a.catalog()
	snap, err := cat.Snapshot()
	if err != nil {
		return "", err
	}
	space, spaceName, group := "", "", ""
	if e, ok := snap.Entry(alias); ok {
		space = e.Space
		if snap.HasGroup(e.Space, e.Group) {
			group = e.Group
		}
	}
	if len(snap.Spaces) > 1 {
		spaceName = snap.SpaceName(space)
	}
	unanswered := fmt.Errorf("%w; set one with: sshctl provision default <url>", noRepo)
	in := bufio.NewReader(a.in)
	fmt.Fprintf(a.out, "Dotfiles repo URL (%s refuses Provision): ", config.DotfilesNone)
	repo, ok := readAnswer(in)
	if !ok || repo == "" {
		fmt.Fprintln(a.out)
		return "", unanswered
	}
	choices := "(g)lobal, "
	if spaceName != "" {
		choices += "(s) space " + spaceName + ", "
	}
	if group != "" {
		choices += "(r) group " + group + ", "
	}
	choices += "(o)nce"
	for {
		fmt.Fprintf(a.out, "Save for: %s: ", choices)
		choice, ok := readAnswer(in)
		if !ok {
			fmt.Fprintln(a.out)
			return "", unanswered
		}
		// none is the refuse setting, never a repo to apply: it is saved
		// like one, and Provision stops.
		var err error
		switch {
		case choice == "g":
			err = cat.SetDotfiles(repo)
		case choice == "s" && spaceName != "":
			err = cat.SetSpaceDotfiles(spaceName, repo)
		case choice == "r" && group != "":
			err = cat.SetGroupDotfiles(space, group, repo)
		case choice != "o":
			continue
		}
		if err == nil && repo == config.DotfilesNone {
			err = catalog.ErrProvisionRefused
		}
		return repo, err
	}
}

// readAnswer reads one line of input, trimmed; ok is false at the end of
// input with nothing typed.
func readAnswer(in *bufio.Reader) (string, bool) {
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimSpace(line), true
}

const provisionDefaultUsage = "usage: sshctl provision default [<url|none> | --unset] [--group <name>]"

// provisionDefault lists the dotfiles settings, or sets or unsets the
// global one or, with --group, a Group's.
func (a *app) provisionDefault(in input) error {
	unset := in.flag("unset")
	group, grouped := in.value(fieldGroup)
	if len(in.args) == 0 && !unset && !grouped {
		return a.listDotfiles()
	}
	var repo string
	if len(in.args) == 1 {
		repo = in.args[0]
	}
	if len(in.args) > 1 || unset == (repo != "") || grouped && group == "" {
		return errors.New(provisionDefaultUsage)
	}
	if grouped {
		return a.catalog().SetGroupDotfiles("", group, repo)
	}
	return a.catalog().SetDotfiles(repo)
}

// listDotfiles prints the global dotfiles setting, then, with several
// Spaces, each Space's, then each Group's in the user's order; a Group of a
// Space other than the default is named space/group.
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
	if len(snap.Spaces) > 1 {
		for _, s := range snap.Spaces {
			fmt.Fprintf(a.out, "space %s: %s\n", s.Name, describeDotfiles(s.Dotfiles))
		}
	}
	for _, g := range snap.Groups {
		name := g.Name
		if g.Space != "" {
			name = g.Space + "/" + g.Name
		}
		fmt.Fprintf(a.out, "group %s: %s\n", name, describeDotfiles(g.Dotfiles))
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
