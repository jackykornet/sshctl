package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
	"github.com/sshctl/sshctl/internal/platform"
)

type app struct {
	in       io.Reader
	out, err io.Writer
	cfgPath  string
	legacy   []string
	cfg      *config.Config
	ssh      *openssh.Client
	// runSSH runs ssh and ssh-keygen; tests replace it.
	runSSH openssh.Runner
}

func run(args []string, in io.Reader, out, errOut io.Writer) error {
	startWorkspace := len(args) == 0 && interactive(in, out)
	if !startWorkspace && (len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		help(out, helpFeatures())
		return nil
	}
	p, legacy, err := config.Paths()
	if err != nil {
		return err
	}
	moved, err := config.MoveOld(p)
	if err != nil {
		return err
	}
	c, err := config.Load(p, legacy)
	if err != nil {
		return err
	}
	if moved != "" {
		c.Notices = append([]string{moved}, c.Notices...)
	}
	s, err := openssh.New()
	if err != nil {
		return err
	}
	a := &app{in: in, out: out, err: errOut, cfgPath: p, legacy: legacy, cfg: c, ssh: s, runSSH: openssh.Run}
	if startWorkspace {
		return a.runWorkspace()
	}
	for _, n := range c.Notices {
		fmt.Fprintln(errOut, "sshctl: "+n)
	}
	return a.execute(args)
}

// helpFeatures returns which Experimental features are on, for help, and
// none when the config cannot be read: help must work anyway.
func helpFeatures() config.Experimental {
	p, legacy, err := config.Paths()
	if err != nil {
		return config.Experimental{}
	}
	c, err := config.Load(p, legacy)
	if err != nil {
		return config.Experimental{}
	}
	return c.Experimental
}

// help prints the command overview; exp marks the Experimental features
// that are off.
func help(w io.Writer, exp config.Experimental) {
	off := func(feature string) string {
		if exp.On(feature) {
			return ""
		}
		return " (experimental, off)"
	}
	bootstrap, provision := off(config.FeatureBootstrap), off(config.FeatureProvision)
	fmt.Fprint(w, `sshctl manages OpenSSH connections and local connection metadata.

Usage: sshctl                 Open the Workspace (in a terminal)
       sshctl <command> [arguments]

Commands:
  list [--space <space>]       List Connections, or one Space's
  connect <alias> [--claude]   Connect using OpenSSH
  add <alias>                  Add or update connection metadata
  edit <alias>                 Edit connection metadata
  remove <alias>               Remove sshctl metadata
  space list                   List Spaces: number, name, groups, connections
  space add <name> [<number>]  Create a Space, with the lowest free number
                               (1-9) or the one given
  space rename <space> <new>   Rename a Space, keeping its contents and number
  space delete <space>         Delete an empty Space, leaving its number free
  space move <space> <number>  Give a Space a number, swapping with the Space
                               that has it; <space> is a Space name or number
  space dotfiles <space> [<url|none>|--unset]
                               Show or set a Space's dotfiles repo`+provision+`
  group [--space <space>] ...  Pick the Space for a group command;
                               <group> is a Group name or number
  group list                   List Groups: number, name, members, dotfiles
  group add <name>             Create a Group
  group rename <group> <new>   Rename a Group, moving its Connections
  group delete <group> [--yes] Delete a Group; its Connections become ungrouped
  group move <group> <pos>     Set a Group's position (its number)
  group connect <group>        Open group connections in terminal windows
  history                      Show recent connections
  key list|use|add             List, select, or register keys;
                               key use --space <space> <key|none> selects a
                               Space's key; none falls back to the global one
  agent ensure                 Inspect SSH agent and optionally add the Selected identity
  known-hosts list|remove      Show or remove known hosts
  bootstrap <alias>            Install your key on the host and use it`+bootstrap+`
  provision <alias>            Apply your dotfiles to the host with chezmoi`+provision+`
  provision default [<url|none>|--unset] [--group <name>]
                               Show or set the global or a Group's dotfiles repo`+provision+`
  experimental list            Show which Experimental features are on
  experimental enable|disable <feature>
                               Turn bootstrap or provision on or off; they are
                               not fully tested yet and start off
  config show                  Show paths and relevant SSH settings

Use sshctl <command> --help for command usage where available.
`)
}
func usage(command string) string {
	switch command {
	case "list":
		return "list [--space <space>]"
	case "connect":
		return "connect <alias> [--claude]"
	case "add", "edit", "remove":
		return command + " <alias>"
	case "group":
		return groupUsage
	case "space":
		return spaceUsage
	case "key":
		return "key list | use [--space <space>] <key-path|none> | add <private-key-path>"
	case "agent":
		return "agent ensure"
	case "known-hosts":
		return "known-hosts list | remove <host>"
	case "provision":
		return provisionUsage
	case "bootstrap":
		return bootstrapUsage
	case "experimental":
		return experimentalUsage
	case "config":
		return "config show"
	default:
		return command
	}
}

func (a *app) execute(args []string) error {
	if args[0] == "--help" || args[0] == "-h" {
		help(a.out, a.cfg.Experimental)
		return nil
	}
	if len(args) > 1 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Fprintf(a.out, "Usage: sshctl %s\n", usage(args[0]))
		return nil
	}
	switch args[0] {
	case "list":
		return a.list(args[1:])
	case "connect":
		return a.connect(args[1:])
	case "add":
		return a.metadata(args[1:], false)
	case "edit":
		return a.metadata(args[1:], true)
	case "remove":
		return a.remove(args[1:])
	case "group":
		return a.groupCommand(args[1:])
	case "space":
		return a.spaceCommand(args[1:])
	case "history":
		return a.history()
	case "key":
		return a.keys(args[1:])
	case "agent":
		if len(args) > 1 && args[1] == "ensure" {
			return a.agentEnsure()
		}
	case "known-hosts":
		return a.knownHosts(args[1:])
	case "provision":
		return a.provisionCommand(args[1:])
	case "bootstrap":
		return a.bootstrapCommand(args[1:])
	case "experimental":
		return a.experimentalCommand(args[1:])
	case "config":
		if len(args) > 1 && args[1] == "show" {
			return a.showConfig()
		}
	}
	return fmt.Errorf("unknown or incomplete command; run sshctl --help")
}
func (a *app) save() error { return config.Save(a.cfgPath, a.cfg) }

// catalog is the Connection catalog over a's config files, read fresh on each call.
func (a *app) catalog() *catalog.Catalog { return catalog.New(a.cfgPath, a.legacy, a.ssh) }

// identity returns the global Selected identity as the catalog holds it
// now, or "" when OpenSSH chooses keys.
func (a *app) identity() (string, error) {
	snap, err := a.catalog().Snapshot()
	return snap.Identity, err
}

// identityFor returns the Selected identity ssh uses for alias as the
// catalog holds it now: its Space's own, or else the global one.
func (a *app) identityFor(alias string) (string, error) { return a.catalog().IdentityFor(alias) }

// list prints the Connections the Workspace lists: ssh_config's aliases, then
// the Orphaned connections, all of them or, with --space, one Space's. When
// sshctl's hosts file is not Included in ssh_config it adds a one-line hint
// on stderr.
func (a *app) list(args []string) error {
	space, rest, err := a.cutSpaceOption(args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return errors.New("usage: sshctl " + usage("list"))
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	spaces, err := space.spaces(snap)
	if err != nil {
		return err
	}
	for _, e := range snap.Entries {
		if space.set && e.Space != spaces[0].ref {
			continue
		}
		var label []string
		if e.Description != "" {
			label = append(label, e.Description)
		}
		if e.Group != "" {
			label = append(label, "["+e.Group+"]")
		}
		if e.Orphaned {
			label = append(label, "(orphaned)")
		}
		if len(label) == 0 {
			fmt.Fprintln(a.out, e.Alias)
			continue
		}
		fmt.Fprintf(a.out, "%-24s %s\n", e.Alias, strings.Join(label, " "))
	}
	if snap.Include != nil {
		fmt.Fprintf(a.err, "hint: %s, so ssh cannot see sshctl's hosts; run sshctl to add it\n", snap.Include)
	}
	return nil
}
func (a *app) connect(args []string) error {
	claude := false
	pos := []string{}
	for _, arg := range args {
		if arg == "--claude" {
			claude = true
		} else {
			pos = append(pos, arg)
		}
	}
	if len(pos) != 1 {
		return errors.New("usage: sshctl connect <alias> [--claude]")
	}
	alias := pos[0]
	key, err := a.identityFor(alias)
	if err != nil {
		return err
	}
	if err := a.ssh.Connect(context.Background(), alias, key, claude); err != nil {
		return err
	}
	return a.recordHistory(alias)
}

// metadata adds or edits alias's description and group, prompting with the
// values the config holds now.
func (a *app) metadata(args []string, edit bool) error {
	if len(args) != 1 {
		return errors.New("usage: sshctl add|edit <alias>")
	}
	alias := args[0]
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	c, _ := snap.Entry(alias)
	if edit && c.Description == "" && c.Group == "" && c.Space == "" {
		return fmt.Errorf("no sshctl metadata for %q", alias)
	}
	reader := bufio.NewReader(a.in)
	fmt.Fprintf(a.out, "Description [%s]: ", c.Description)
	desc, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(desc) != "" {
		c.Description = strings.TrimSpace(desc)
	}
	fmt.Fprintf(a.out, "Group [%s]: ", c.Group)
	group, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(group) != "" {
		c.Group = strings.TrimSpace(group)
	}
	// Saving under the same alias without Managed changes only the
	// description and group, never a Host block.
	err = a.catalog().Save(alias, catalog.Spec{Alias: alias, Description: c.Description, Space: c.Space, Group: c.Group})
	if errors.Is(err, catalog.ErrUnknownGroup) {
		return fmt.Errorf("no group %q; create it first with: sshctl group add %s", c.Group, c.Group)
	}
	return err
}
func (a *app) remove(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: sshctl remove <alias>")
	}
	// Only the metadata goes: a Host block, even sshctl's, stays.
	err := a.catalog().DeleteMetadata(args[0])
	if errors.Is(err, catalog.ErrNoMetadata) {
		return fmt.Errorf("no sshctl metadata for %q", args[0])
	}
	return err
}
func (a *app) groupConnect(space spaceOption, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: sshctl group [--space <space>] connect <group>")
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	ref, group, err := findGroup(snap, space, args[0])
	if err != nil {
		return err
	}
	var names []string
	for _, e := range snap.Entries {
		if e.Space == ref && e.Group == group {
			names = append(names, e.Alias)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("group %q has no Connections", group)
	}
	for _, n := range names {
		if err := platform.OpenSSH(n, snap.SpaceIdentity(ref)); err != nil {
			return err
		}
	}
	return nil
}
func confirm(in io.Reader) bool {
	line, err := bufio.NewReader(in).ReadString('\n')
	return err == nil && (strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes"))
}
func (a *app) showConfig() error {
	key, err := a.identity()
	if err != nil {
		return err
	}
	if key == "" {
		key = "none (OpenSSH chooses keys)"
	}
	fmt.Fprintf(a.out, "sshctl config: %s\nsshctl state: %s\nOpenSSH config: %s\nKnown hosts: %s\nSelected identity: %s\n", a.cfgPath, config.StatePath(a.cfgPath), a.ssh.SSHConfig, openssh.KnownHostsPath(), key)
	settings, err := a.ssh.SafeSettings()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.out, "OpenSSH settings:")
	for _, setting := range settings {
		fmt.Fprintln(a.out, "  "+setting)
	}
	return nil
}
