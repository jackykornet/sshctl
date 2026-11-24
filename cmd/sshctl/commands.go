package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
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
	// openTerminal opens a terminal window running ssh to alias, with key
	// when set, for group connect; nil means platform.OpenSSH. Tests
	// replace it.
	openTerminal func(alias, key string) error
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

// command is one sshctl command: its name, the forms it is called in, and,
// when inSpace is set, the --space option, which picks the Space for every
// form and may come before the form's subcommand.
type command struct {
	name    string
	inSpace bool
	// spaceHelp is the help for --space, on a line of its own.
	spaceHelp string
	// note follows the usage in the command's own --help and in its usage
	// errors, for what the usage line cannot say.
	note  string
	forms []form
}

// form is one way to call a command: its subcommand ("" for none), the
// arguments after it, a help line (with "\n" between wrapped lines) and the
// Experimental feature it needs, if any. run gets the arguments after the
// subcommand, --space included; it returns errUsage for arguments that do
// not fit, and execute turns that into the command's usage.
type form struct {
	sub, args, help, feature string
	run                      func(a *app, args []string) error
}

// spaceOptionUsage is how help shows the --space option.
const spaceOptionUsage = "[--space <space>]"

// errUsage is returned by a form for arguments that do not fit it.
var errUsage = errors.New("usage")

// commands is every sshctl command, in the order help lists them. It drives
// both dispatch (execute) and help, and the README's Commands block lists
// its forms in the same order.
var commands = []command{
	{name: "list", forms: []form{
		{args: spaceOptionUsage, help: "List Connections, or one Space's", run: (*app).list},
	}},
	{name: "connect", forms: []form{
		{args: "<alias>", help: "Connect using OpenSSH", run: (*app).connect},
	}},
	{name: "tunnel", forms: []form{
		{sub: "add", args: "<alias> <spec>", help: "Save a Tunnel, [name=]localport[:host:remoteport];\nthe host defaults to localhost", run: (*app).tunnelAdd},
		{sub: "rm", args: "<alias> <name|port>", help: "Delete a Tunnel by name or local port", run: (*app).tunnelRemove},
		{sub: "list", args: "[<alias>]", help: "List a Connection's Tunnels, or every one's", run: (*app).tunnelList},
		{args: "<alias>", help: "Run a Connection's Tunnels until Ctrl-C", run: (*app).tunnelRun},
	}},
	{name: "add", note: connNote, forms: []form{
		{args: "<alias> [--<field> <value>]...", help: "Add a Connection: its Host block, written to\n~/.ssh/sshctl_config, and its metadata", run: (*app).add},
	}},
	{name: "edit", note: connNote, forms: []form{
		{args: "<alias> [--<field> <value>]...", help: "Edit a Connection's fields; for one defined\noutside sshctl, only its metadata", run: (*app).edit},
	}},
	{name: "remove", forms: []form{
		{args: "<alias> [--yes]", help: "Remove a Connection: its Host block if sshctl\nwrote it, and its metadata", run: (*app).remove},
	}},
	{name: "space", forms: []form{
		{sub: "list", help: "List Spaces: number, name, groups, connections", run: (*app).spaceList},
		{sub: "add", args: "<name> [<number>]", help: "Create a Space, with the lowest free number\n(1-9) or the one given", run: (*app).spaceAdd},
		{sub: "rename", args: "<space> <new>", help: "Rename a Space, keeping its contents and number", run: (*app).spaceRename},
		{sub: "delete", args: "<space>", help: "Delete an empty Space, leaving its number free", run: (*app).spaceDelete},
		{sub: "move", args: "<space> <number>", help: "Give a Space a number, swapping with the Space\nthat has it; <space> is a Space name or number", run: (*app).spaceMove},
		{sub: "dotfiles", args: "<space> [<url|none> | --unset]", help: "Show or set a Space's dotfiles repo", feature: config.FeatureProvision, run: (*app).spaceDotfiles},
	}},
	{name: "group", inSpace: true, spaceHelp: "Pick the Space for a group command;\n<group> is a Group name or number",
		// group connect opens OS terminal windows (internal/platform),
		// unlike prefix g in the Workspace, which opens tabs.
		note: "group connect opens one terminal window per Connection in the Group;\nto open them as tabs, use prefix g in the Workspace (run sshctl with no arguments).", forms: []form{
			{sub: "list", help: "List Groups: number, name, members, dotfiles", run: inSpace((*app).groupList)},
			{sub: "add", args: "<name>", help: "Create a Group", run: inSpace((*app).groupAdd)},
			{sub: "rename", args: "<group> <new>", help: "Rename a Group, moving its Connections", run: inSpace((*app).groupRename)},
			{sub: "delete", args: "<group> [--yes]", help: "Delete a Group; its Connections become ungrouped", run: inSpace((*app).groupDelete)},
			{sub: "move", args: "<group> <position>", help: "Set a Group's position (its number)", run: inSpace((*app).groupMove)},
			{sub: "connect", args: "<group>", help: "Open one terminal window per Connection in the\nGroup; for tabs, use prefix g in the Workspace", run: inSpace((*app).groupConnect)},
		}},
	{name: "history", forms: []form{
		{help: "Show recent connections", run: func(a *app, _ []string) error { return a.history() }},
	}},
	{name: "key", forms: []form{
		{sub: "list", help: "List the keys in ~/.ssh and the registered ones", run: (*app).keyList},
		{sub: "use", args: spaceOptionUsage + " <key-path|none>", help: "Select the key sshctl uses, or with --space a\nSpace's; none falls back to the global one", run: (*app).keyUse},
		{sub: "add", args: "<private-key-path>", help: "Register a key path", run: (*app).keyAdd},
	}},
	{name: "agent", forms: []form{
		{sub: "ensure", help: "Inspect SSH agent and optionally add the Selected identity", run: func(a *app, _ []string) error { return a.agentEnsure() }},
	}},
	{name: "known-hosts", forms: []form{
		{sub: "list", help: "Show known hosts", run: (*app).knownHostsList},
		{sub: "remove", args: "<host>", help: "Remove a known host", run: (*app).knownHostsRemove},
	}},
	{name: "bootstrap", forms: []form{
		{args: "<alias>", help: "Install your key on the host and use it", feature: config.FeatureBootstrap, run: (*app).bootstrapCommand},
	}},
	{name: "provision", forms: []form{
		{args: "<alias>", help: "Apply your dotfiles to the host with chezmoi", feature: config.FeatureProvision, run: (*app).provisionCommand},
		{sub: "default", args: "[<url|none> | --unset] [--group <name>]", help: "Show or set the global or a Group's dotfiles repo", feature: config.FeatureProvision, run: (*app).provisionDefault},
	}},
	{name: "experimental", forms: []form{
		{sub: "list", help: "Show which Experimental features are on", run: (*app).experimentalList},
		{args: "enable|disable <feature>", help: "Turn bootstrap or provision on or off; they are\nnot fully tested yet and start off", run: (*app).experimentalSet},
	}},
	{name: "config", forms: []form{
		{sub: "show", help: "Show paths and relevant SSH settings", run: func(a *app, _ []string) error { return a.showConfig() }},
	}},
}

// inSpace adapts a command that takes --space to a form: it takes --space,
// wherever it is, out of the arguments.
func inSpace(run func(a *app, space spaceOption, args []string) error) func(*app, []string) error {
	return func(a *app, args []string) error {
		space, rest, err := a.cutSpaceOption(args)
		if err != nil {
			return err
		}
		return run(a, space, rest)
	}
}

// call is how help shows f: its subcommand and arguments.
func (f form) call() string { return strings.TrimSpace(f.sub + " " + f.args) }

// prefix is c's name and, if it takes one, its --space option.
func (c command) prefix() string {
	if c.inSpace {
		return c.name + " " + spaceOptionUsage
	}
	return c.name
}

// synopsis is how f of c is called, after "sshctl ".
func (c command) synopsis(f form) string { return strings.TrimSpace(c.prefix() + " " + f.call()) }

// usage is how c is called, after "sshctl ": every form, after the option.
func (c command) usage() string {
	calls := make([]string, len(c.forms))
	for i, f := range c.forms {
		calls[i] = f.call()
	}
	return strings.TrimSpace(c.prefix() + " " + strings.Join(calls, " | "))
}

// usageText is the command's usage followed by its note, if it has one.
func (c command) usageText() string {
	if c.note == "" {
		return c.usage()
	}
	return c.usage() + "\n\n" + c.note
}

// usage is how the command named name is called, after "sshctl ", with its
// note.
func usage(name string) string {
	if c := findCommand(name); c != nil {
		return c.usageText()
	}
	return name
}

// findCommand returns the command named name, or nil.
func findCommand(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// lookup finds the command and form that args call, and returns the
// arguments for the form: those after its subcommand, with any --space
// before it. A form without a subcommand takes what no other form of the
// command does. It returns a nil command for an unknown one, and a nil form
// when no form fits.
func lookup(args []string) (*command, *form, []string) {
	if len(args) == 0 {
		return nil, nil, nil
	}
	c := findCommand(args[0])
	if c == nil {
		return nil, nil, nil
	}
	var opts []string
	rest := args[1:]
	for c.inSpace && len(rest) > 0 && rest[0] == "--space" {
		n := min(2, len(rest))
		opts, rest = append(opts, rest[:n]...), rest[n:]
	}
	for i := range c.forms {
		if f := &c.forms[i]; f.sub != "" && len(rest) > 0 && rest[0] == f.sub {
			return c, f, append(opts, rest[1:]...)
		}
	}
	for i := range c.forms {
		if f := &c.forms[i]; f.sub == "" {
			return c, f, append(opts, rest...)
		}
	}
	return c, nil, nil
}

// helpColumn is the width of help's first column, the calls.
const helpColumn = 29

// help prints the command overview; exp marks the Experimental features
// that are off.
func help(w io.Writer, exp config.Experimental) {
	fmt.Fprint(w, `sshctl manages OpenSSH connections and local connection metadata.

Usage: sshctl                 Open the Workspace (in a terminal)
       sshctl <command> [arguments]

Commands:
`)
	for _, c := range commands {
		if c.inSpace {
			helpLine(w, c.prefix()+" ...", c.spaceHelp)
		}
		for _, f := range c.forms {
			text := f.help
			if f.feature != "" && !exp.On(f.feature) {
				text += " (experimental, off)"
			}
			helpLine(w, strings.TrimSpace(c.name+" "+f.call()), text)
		}
	}
	fmt.Fprint(w, `
Use sshctl <command> --help for command usage.
`)
}

// helpLine prints call and its help text in two columns; a call too long
// for the first column gets a line of its own.
func helpLine(w io.Writer, call, text string) {
	indent := strings.Repeat(" ", 2+helpColumn)
	if len(call) < helpColumn {
		fmt.Fprintf(w, "  %-*s", helpColumn, call)
	} else {
		fmt.Fprintf(w, "  %s\n%s", call, indent)
	}
	fmt.Fprintln(w, strings.ReplaceAll(text, "\n", "\n"+indent))
}

// execute runs the command args call, through the commands table.
func (a *app) execute(args []string) error {
	if args[0] == "--help" || args[0] == "-h" {
		help(a.out, a.cfg.Experimental)
		return nil
	}
	c, f, rest := lookup(args)
	if c == nil {
		return errors.New("unknown or incomplete command; run sshctl --help")
	}
	if len(args) > 1 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Fprintf(a.out, "Usage: sshctl %s\n", c.usageText())
		return nil
	}
	if f == nil {
		return errors.New("usage: sshctl " + c.usageText())
	}
	if f.feature != "" {
		if err := a.checkFeature(f.feature); err != nil {
			return err
		}
	}
	err := f.run(a, rest)
	if errors.Is(err, errUsage) {
		return errors.New("usage: sshctl " + c.usageText())
	}
	return err
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
		return errUsage
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	for _, e := range snap.Entries {
		if space.set && e.Space != space.ref {
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
			label = append(label, "(missing)")
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
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errUsage
	}
	alias := args[0]
	key, err := a.identityFor(alias)
	if err != nil {
		return err
	}
	if err := a.ssh.Connect(context.Background(), alias, key); err != nil {
		return err
	}
	return a.recordHistory(alias)
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
