package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
)

// The Connection fields add and edit take, as flags and prompts, in the
// Workspace form's order (ADR-0008).
const (
	fieldAlias       = "alias"
	fieldHost        = "host"
	fieldUser        = "user"
	fieldPort        = "port"
	fieldIdentity    = "identity"
	fieldDescription = "description"
	fieldSpace       = "space"
	fieldGroup       = "group"
)

// connField is one Connection field: its flag, its prompt label, and
// whether it is part of the Host block, so only a Managed connection has it.
type connField struct {
	flag, label string
	host        bool
}

var connFields = []connField{
	{fieldAlias, "Alias", true},
	{fieldHost, "HostName", true},
	{fieldUser, "User", true},
	{fieldPort, "Port", true},
	{fieldIdentity, "Identity file", true},
	{fieldDescription, "Description", false},
	{fieldSpace, "Space", false},
	{fieldGroup, "Group", false},
}

// connNote is the note add's and edit's usage carries: the field flags.
const connNote = "Fields: --host <hostname> --user <user> --port <port> --identity <file>\n" +
	"--description <text> --space <space> --group <group>; edit also takes\n" +
	"--alias <new> to rename. Without flags, each field is prompted for and an\n" +
	"empty answer keeps the current value; --<field> \"\" clears it. <space> and\n" +
	"<group> are a name or number; --group 0 is ungrouped."

// parseConnArgs returns the alias and the field flags args give, by flag
// name. It returns errUsage for anything else, a flag given twice or without
// a value, and --alias unless rename is set.
func parseConnArgs(args []string, rename bool) (string, map[string]string, error) {
	var alias string
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			if alias != "" {
				return "", nil, errUsage
			}
			alias = arg
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		known := false
		for _, f := range connFields {
			known = known || f.flag == name
		}
		if !known || (name == fieldAlias && !rename) {
			return "", nil, errUsage
		}
		if _, dup := flags[name]; dup {
			return "", nil, errUsage
		}
		if !hasValue {
			if i+1 == len(args) {
				return "", nil, errUsage
			}
			i++
			value = args[i]
		}
		flags[name] = value
	}
	if alias == "" {
		return "", nil, errUsage
	}
	return alias, flags, nil
}

// add adds a new Connection, a Managed one whose Host block sshctl writes,
// from flags or, without any, prompts.
func (a *app) add(args []string) error { return a.saveConnection(args, false) }

// edit changes an existing Connection's fields, from flags or, without any,
// prompts holding the current values.
func (a *app) edit(args []string) error { return a.saveConnection(args, true) }

// saveConnection is add (edit unset) and edit. Both save through
// catalog.Save with a Spec, as the Workspace form does, and look up the
// Space and Group with the catalog's resolver (ADR-0008).
func (a *app) saveConnection(args []string, edit bool) error {
	alias, flags, err := parseConnArgs(args, edit)
	if err != nil {
		return err
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	e, exists := snap.Entry(alias)
	managed := true
	switch {
	case !edit && exists && !e.Orphaned:
		return fmt.Errorf("%s already exists in ssh_config; change it with: sshctl edit %s", alias, alias)
	case edit && !exists:
		return fmt.Errorf("no connection %q; add it with: sshctl add %s", alias, alias)
	case edit:
		managed = e.Managed
	}
	if !managed {
		why := alias + "'s Host block is defined outside sshctl"
		if e.Orphaned {
			why = alias + " has no Host block in ssh_config"
		}
		for _, f := range connFields {
			if _, ok := flags[f.flag]; ok && f.host {
				return fmt.Errorf("%s; only its description, Space and group can be changed here", why)
			}
		}
		fmt.Fprintf(a.out, "%s; changing only its description, Space and group.\n", why)
	}
	values := map[string]string{
		fieldAlias: alias, fieldHost: e.Host.HostName, fieldUser: e.Host.User, fieldPort: e.Host.Port,
		fieldIdentity: e.Host.IdentityFile, fieldDescription: e.Description,
		fieldSpace: snap.SpaceName(e.Space), fieldGroup: e.Group,
	}
	reader := bufio.NewReader(a.in)
	space := e.Space
	if len(flags) == 0 {
		if space, err = a.promptFields(reader, snap, values, edit, managed, e.Space); err != nil {
			return err
		}
	} else {
		if input, ok := flags[fieldSpace]; ok {
			if space, err = snap.ResolveSpace(input); err != nil {
				return err
			}
		}
		if _, ok := flags[fieldGroup]; !ok && space != e.Space {
			values[fieldGroup] = ""
		}
		for name, v := range flags {
			values[name] = v
		}
	}
	group := values[fieldGroup]
	if group != "" {
		if group, err = snap.ResolveGroup(space, group); err != nil {
			return groupHint(err, snap, space, values[fieldGroup])
		}
	}
	old := ""
	if edit {
		old = alias
	}
	s := catalog.Spec{
		Alias: strings.TrimSpace(values[fieldAlias]), Description: values[fieldDescription],
		Space: space, Group: group, Managed: managed,
		Host: catalog.Host{HostName: values[fieldHost], User: values[fieldUser], Port: values[fieldPort], IdentityFile: values[fieldIdentity]},
		// Tunnels have no field here; sshctl tunnel changes them, and they
		// stay as they are (ADR-0012).
		Tunnels: e.Tunnels,
	}
	if err := a.saveSpec(reader, old, s); err != nil {
		return err
	}
	verb := "saved"
	if !edit {
		verb = "added"
	}
	fmt.Fprintf(a.out, "%s %s\n", verb, s.Alias)
	return nil
}

// promptFields asks for each field the Connection has, holding values,
// and sets the answers that are not empty. The Space is asked for only when
// there are several, and a change of Space clears the Group before it is
// asked for. It returns the Space, as Connections refer to it.
func (a *app) promptFields(r *bufio.Reader, snap catalog.Snapshot, values map[string]string, edit, managed bool, space string) (string, error) {
	for _, f := range connFields {
		if f.host && !managed || f.flag == fieldAlias && !edit || f.flag == fieldSpace && len(snap.Spaces) < 2 {
			continue
		}
		fmt.Fprintf(a.out, "%s [%s]: ", f.label, values[f.flag])
		answer, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		if answer == "" {
			continue
		}
		if f.flag == fieldSpace {
			ref, err := snap.ResolveSpace(answer)
			if err != nil {
				return "", err
			}
			if ref != space {
				values[fieldGroup] = ""
			}
			space = ref
		}
		values[f.flag] = answer
	}
	return space, nil
}

// groupHint adds to an unknown Group error how to create the Group.
func groupHint(err error, snap catalog.Snapshot, space, group string) error {
	if !errors.Is(err, catalog.ErrUnknownGroup) {
		return err
	}
	cmd := "sshctl group add " + group
	if space != "" {
		cmd = "sshctl group --space " + snap.SpaceName(space) + " add " + group
	}
	return fmt.Errorf("%w; create it first with: %s", err, cmd)
}

// saveSpec saves s in place of old through the catalog. When ssh_config
// must first Include sshctl's file it asks, as the Workspace does, and adds
// the line before saving again.
func (a *app) saveSpec(r *bufio.Reader, old string, s catalog.Spec) error {
	err := a.catalog().Save(old, s)
	var inc *catalog.IncludeError
	if !errors.As(err, &inc) {
		return err
	}
	fmt.Fprintf(a.out, "Add %q to the top of %s? (y/n) ", inc.Line, inc.Config)
	if !confirmed(r) {
		return err
	}
	if err := a.catalog().AddInclude(); err != nil {
		return err
	}
	return a.catalog().Save(old, s)
}

// remove deletes a Connection as the Workspace's d does: its Host block
// when sshctl manages it, and its sshctl metadata, after asking unless
// --yes is given. A Host block elsewhere in ssh_config is the user's.
func (a *app) remove(args []string) error {
	var alias string
	yes := false
	for _, arg := range args {
		switch {
		case arg == "--yes":
			yes = true
		case alias == "" && !strings.HasPrefix(arg, "-"):
			alias = arg
		default:
			return errUsage
		}
	}
	if alias == "" {
		return errUsage
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	e, ok := snap.Entry(alias)
	if !ok {
		return fmt.Errorf("no connection %q", alias)
	}
	question := fmt.Sprintf("Delete %s from ssh_config? (y/n) ", alias)
	if !e.Managed {
		if e.Description == "" && e.Group == "" && e.Space == "" && len(e.Tunnels) == 0 {
			return fmt.Errorf("%s is defined outside sshctl; edit your ssh_config to remove it", alias)
		}
		question = fmt.Sprintf("Remove %s's description, group and tunnels? Its Host block is not sshctl's. (y/n) ", alias)
	}
	if !yes {
		fmt.Fprint(a.out, question)
		if !confirmed(bufio.NewReader(a.in)) {
			return errors.New("not removed")
		}
	}
	return a.catalog().Delete(alias)
}

// confirmed reads a line from r and reports whether it is y or yes.
func confirmed(r *bufio.Reader) bool {
	line, err := r.ReadString('\n')
	line = strings.TrimSpace(line)
	return (err == nil || line != "") && (strings.EqualFold(line, "y") || strings.EqualFold(line, "yes"))
}
