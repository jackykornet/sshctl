package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
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
	if errors.Is(err, catalog.ErrNoDotfiles) {
		repo, err = a.askDotfiles(alias, err)
	}
	if err != nil {
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

// askDotfiles asks for the dotfiles repo when none applies to alias (noRepo
// is the catalog's ErrNoDotfiles), then whether to save it as the global
// setting, as the setting of alias's Group (offered only when it has one),
// or not at all. It reads a plain terminal, so it works in a Workspace tab
// too. Without an answer it returns noRepo with a hint and saves nothing.
func (a *app) askDotfiles(alias string, noRepo error) (string, error) {
	cat := a.catalog()
	snap, err := cat.Snapshot()
	if err != nil {
		return "", err
	}
	group := ""
	for _, e := range snap.Entries {
		if e.Alias == alias && slices.ContainsFunc(snap.Groups, func(g catalog.Group) bool { return g.Name == e.Group }) {
			group = e.Group
		}
	}
	unanswered := fmt.Errorf("%w; set one with: sshctl provision default <url>", noRepo)
	in := bufio.NewReader(a.in)
	fmt.Fprint(a.out, "Dotfiles repo URL: ")
	repo, ok := readAnswer(in)
	if !ok || repo == "" {
		fmt.Fprintln(a.out)
		return "", unanswered
	}
	choices := "(g)lobal, (o)nce"
	if group != "" {
		choices = "(g)lobal, (r) group " + group + ", (o)nce"
	}
	for {
		fmt.Fprintf(a.out, "Save for: %s: ", choices)
		choice, ok := readAnswer(in)
		if !ok {
			fmt.Fprintln(a.out)
			return "", unanswered
		}
		switch {
		case choice == "g":
			return repo, cat.SetDotfiles(repo)
		case choice == "r" && group != "":
			return repo, cat.SetGroupDotfiles(group, repo)
		case choice == "o":
			return repo, nil
		}
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
