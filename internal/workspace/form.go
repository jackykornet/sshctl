package workspace

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/tunnel"
)

// Host is how OpenSSH reaches a Connection whose Host block sshctl manages.
type Host = catalog.Host

// Spec is a Connection as entered in the connection form.
type Spec = catalog.Spec

// Tunnel is a saved local forward on a Connection (ADR-0012).
type Tunnel = catalog.Tunnel

// IncludeError is returned by Catalog.Save when the user's ssh_config does
// not yet read sshctl's hosts file. Saving can be retried after
// Catalog.AddInclude adds Line to Config.
type IncludeError = catalog.IncludeError

type fieldID int

const (
	fieldAlias fieldID = iota
	fieldHostName
	fieldUser
	fieldPort
	fieldIdentity
	fieldDescription
	fieldSpace
	fieldGroup
	fieldTunnels
)

type formField struct {
	id          fieldID
	label       string
	placeholder string
	value       line
}

// connForm is the form for adding or editing a Connection, drawn in the pane.
type connForm struct {
	prefixGate
	// old is the alias being edited, or "" when adding.
	old string
	// space is the Space the Connection is saved in, "" for the default
	// Space. With several Spaces the form has a Space field, which sets it
	// on save.
	space   string
	managed bool
	fields  []formField
	focus   int
	err     string
}

// newAddForm is the form for a new Connection in space ("" for the default
// Space), with group filled in.
func newAddForm(space, group string) *connForm {
	return &connForm{space: space, managed: true, fields: managedFields(Spec{Group: group})}
}

func newEditForm(c *Connection) *connForm {
	s := catalog.SpecFor(c.Entry)
	f := &connForm{old: c.Alias, space: c.Space, managed: c.Managed}
	if c.Managed {
		f.fields = managedFields(s)
	} else {
		f.fields = []formField{
			{id: fieldDescription, label: "Description", value: newLine(s.Description)},
			{id: fieldGroup, label: "Group", placeholder: "none", value: newLine(s.Group)},
			tunnelsField(s.Tunnels),
		}
	}
	return f
}

// addSpaceField adds the Space field, holding name, before the Group field.
func (f *connForm) addSpaceField(name string) {
	i := slices.IndexFunc(f.fields, func(fl formField) bool { return fl.id == fieldGroup })
	f.fields = slices.Insert(f.fields, i, formField{id: fieldSpace, label: "Space", placeholder: "name or number", value: newLine(name)})
}

// focusOn moves the focus to field id, if the form has it.
func (f *connForm) focusOn(id fieldID) {
	if i := slices.IndexFunc(f.fields, func(fl formField) bool { return fl.id == id }); i >= 0 {
		f.focusField(i)
	}
}

// field returns the form's field id, or nil when it has none.
func (f *connForm) field(id fieldID) *formField {
	for i := range f.fields {
		if f.fields[i].id == id {
			return &f.fields[i]
		}
	}
	return nil
}

// targetSpace returns the Space the form saves into, as Connections refer
// to it: what the Space field names, or else the form's own Space. ok is
// false when the Space field names no Space.
func (f *connForm) targetSpace(st *State) (ref string, ok bool) {
	fl := f.field(fieldSpace)
	if fl == nil {
		return f.space, true
	}
	return st.ResolveSpace(strings.TrimSpace(fl.value.String()))
}

// clearGroupIfSpaceChanged clears the Group after the Space field's value
// changed from before, so the Group is chosen from the new Space.
func (f *connForm) clearGroupIfSpaceChanged(fl *formField, before string) {
	if fl.id == fieldSpace && fl.value.String() != before {
		f.field(fieldGroup).value = newLine("")
	}
}

// groups returns the stored Groups of the Space the form saves into, for
// the Group numbers under its Group field; none while the Space field names
// no Space.
func (f *connForm) groups(st *State) []string {
	if ref, ok := f.targetSpace(st); ok {
		return st.SpaceGroups(ref)
	}
	return nil
}

func managedFields(s Spec) []formField {
	return []formField{
		{id: fieldAlias, label: "Alias", placeholder: "name used with ssh", value: newLine(s.Alias)},
		{id: fieldHostName, label: "HostName", placeholder: "host or IP address", value: newLine(s.Host.HostName)},
		{id: fieldUser, label: "User", placeholder: "OpenSSH default", value: newLine(s.Host.User)},
		{id: fieldPort, label: "Port", placeholder: "22", value: newLine(s.Host.Port)},
		{id: fieldIdentity, label: "Identity file", placeholder: "OpenSSH default", value: newLine(s.Host.IdentityFile)},
		{id: fieldDescription, label: "Description", value: newLine(s.Description)},
		{id: fieldGroup, label: "Group", placeholder: "none", value: newLine(s.Group)},
		tunnelsField(s.Tunnels),
	}
}

// tunnelsField is the Tunnels field: a comma-separated list of specs, for
// every Connection, since Tunnels are sshctl metadata (ADR-0012).
func tunnelsField(tunnels []Tunnel) formField {
	return formField{id: fieldTunnels, label: "Tunnels", placeholder: "panel=8083, cockpit=9090", value: newLine(tunnel.Join(tunnels))}
}

func (f *connForm) title() string {
	switch {
	case f.old == "":
		return "Add connection"
	case f.managed:
		return "Edit " + f.old
	default:
		return "Edit " + f.old + " (sshctl metadata)"
	}
}

// spec returns the form's values with surrounding spaces trimmed, all but
// the Tunnels, which tunnels parses.
func (f *connForm) spec() Spec {
	s := Spec{Alias: f.old, Space: f.space, Managed: f.managed}
	for _, fl := range f.fields {
		v := strings.TrimSpace(fl.value.String())
		switch fl.id {
		case fieldAlias:
			s.Alias = v
		case fieldHostName:
			s.Host.HostName = v
		case fieldUser:
			s.Host.User = v
		case fieldPort:
			s.Host.Port = v
		case fieldIdentity:
			s.Host.IdentityFile = v
		case fieldDescription:
			s.Description = v
		case fieldGroup:
			s.Group = v
		}
	}
	return s
}

// tunnels parses the Tunnels field, a comma-separated list of specs.
func (f *connForm) tunnels() ([]Tunnel, error) {
	return tunnel.ParseList(f.field(fieldTunnels).value.String())
}

func (m *Model) startAdd() tea.Cmd {
	group := ""
	if r, ok := m.st.CursorRow(); ok {
		group = r.Group
	}
	f := newAddForm(m.st.Space(), group)
	m.withSpaceField(f)
	m.openModal(f)
	return nil
}

// withSpaceField gives f a Space field, holding its Space's name, when
// there is more than the default Space.
func (m *Model) withSpaceField(f *connForm) {
	if m.st.SeveralSpaces() {
		f.addSpaceField(m.st.SpaceName(f.space))
	}
}

func (m *Model) startEdit() tea.Cmd { return m.startEditAt(fieldAlias) }

// startEditAt opens the edit form for the target Connection with the focus
// on field focus, or on its first field when it has no such field.
func (m *Model) startEditAt(focus fieldID) tea.Cmd {
	c := m.moveTarget()
	if c == nil {
		return m.setStatus("no connection to edit", false)
	}
	f := newEditForm(c)
	m.withSpaceField(f)
	f.focusOn(focus)
	m.openModal(f)
	return nil
}

func (f *connForm) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := f.intercept(m, msg); ok {
		return cmd
	}
	if bk, ok := findKey(f.keys(), msg.String()); ok {
		return bk.run(m, msg)
	}
	fl := &f.fields[f.focus]
	before := fl.value.String()
	fl.value.key(msg)
	f.clearGroupIfSpaceChanged(fl, before)
	return nil
}

func (f *connForm) keys() []boundKey {
	focus := func(delta int) action {
		return func(*Model, tea.KeyPressMsg) tea.Cmd { f.focusField(f.focus + delta); return nil }
	}
	save := func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.saveForm(f, false) }
	keys := []boundKey{
		on("ctrl+s", save).as("save"),
		on("tab", focus(1)).as("move"), on("down", focus(1)),
		on("shift+tab", focus(-1)).as("move"), on("up", focus(-1)),
		on("enter", func(m *Model, msg tea.KeyPressMsg) tea.Cmd {
			if f.focus < len(f.fields)-1 {
				return focus(1)(m, msg)
			}
			return save(m, msg)
		}),
		on("esc", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.showModal(nil); return nil }).as("cancel"),
	}
	if f.old == "" {
		keys = append(keys, on("ctrl+o", func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			// While Bootstrap is off, nothing is saved and the form
			// stays open, saying why.
			if err := m.features.Check(catalog.FeatureBootstrap); err != nil {
				f.err = err.Error()
				return nil
			}
			return m.saveForm(f, true)
		}).as("save and Bootstrap"))
	}
	return keys
}

// hints are the form's hint lines: its own keys, then Ctrl+O on a new
// Connection, then how to leave after a swallowed prefix command.
func (f *connForm) hints() []hintLine {
	out := []hintLine{hintsFor(f.keys(), "ctrl+s", "tab", "shift+tab", "esc")}
	if f.old == "" {
		out = append(out, hintsFor(f.keys(), "ctrl+o"))
	}
	if f.swallowed {
		out = append(out, hintsFor(f.keys(), "esc"))
	}
	return out
}

// focusField moves the focus to field i, wrapping around, with the cursor
// at the end of its value.
func (f *connForm) focusField(i int) {
	f.focus = (i + len(f.fields)) % len(f.fields)
	f.fields[f.focus].value.end()
}

func (f *connForm) paste(text string) {
	fl := &f.fields[f.focus]
	before := fl.value.String()
	fl.value.paste(text)
	f.clearGroupIfSpaceChanged(fl, before)
}

func (f *connForm) status() (string, bool) {
	if f.swallowed {
		return "form open  " + hintsFor(f.keys(), "esc").String(), true
	}
	return "", false
}

// saveForm saves the form through the Catalog and reloads the
// list. Errors stay in the form so they can be corrected; when ssh_config
// must Include sshctl's hosts file first, it asks before adding the line.
// With bootstrap, a successful save then opens a Bootstrap tab for the
// saved alias.
func (m *Model) saveForm(f *connForm, bootstrap bool) tea.Cmd {
	space, ok := f.targetSpace(m.st)
	if !ok {
		f.err = "no space " + strings.TrimSpace(f.field(fieldSpace).value.String())
		return nil
	}
	f.space = space
	s := f.spec()
	tunnels, err := f.tunnels()
	if err != nil {
		f.err = err.Error()
		f.focusOn(fieldTunnels)
		return nil
	}
	s.Tunnels = tunnels
	group, ok := m.st.ResolveGroupIn(space, s.Group)
	if !ok {
		f.err = "no group number " + s.Group
		return nil
	}
	s.Group = group
	if f.old != "" && s.Alias != f.old {
		if c := m.st.Get(f.old); c != nil && c.Open() {
			f.err = "close " + f.old + "'s tabs before renaming it"
			return nil
		}
	}
	err = m.opts.Catalog.Save(f.old, s)
	var inc *IncludeError
	if errors.As(err, &inc) {
		m.askInclude(inc, func(m *Model) tea.Cmd {
			if err := m.opts.Catalog.AddInclude(); err != nil {
				f.err = err.Error()
				return nil
			}
			return m.saveForm(f, bootstrap)
		})
		return nil
	}
	if errors.Is(err, catalog.ErrUnknownGroup) {
		f.err = "no group " + s.Group
		m.askCreateGroup(space, s.Group, func(m *Model) tea.Cmd { return m.saveForm(f, bootstrap) })
		return nil
	}
	if err != nil {
		f.err = err.Error()
		return nil
	}
	m.showModal(nil)
	verb := "saved"
	if f.old == "" {
		verb = "added"
	}
	status := fmt.Sprintf("%s %s", verb, s.Alias)
	moved := space != m.st.Space()
	if moved {
		status += " in space " + m.st.SpaceName(space)
	}
	cmd := m.reload(s.Alias, status)
	if moved {
		// Show the Connection where it now is, with any tabs it has.
		cmd = tea.Batch(cmd, m.showSpace(space))
		m.st.SetCursor(s.Alias)
	}
	if bootstrap {
		return tea.Batch(cmd, m.open(s.Alias, Launch{Action: ActionBootstrap}))
	}
	return cmd
}

// askInclude asks before adding inc's Include line to the user's ssh_config
// (ADR-0002); yes runs on y.
func (m *Model) askInclude(inc *IncludeError, yes func(*Model) tea.Cmd) {
	m.ask(fmt.Sprintf("Add %q to the top of %s?", inc.Line, inc.Config), yes)
}

// offerInclude asks at startup to add the Include that sshctl's existing
// hosts file lacks (ADR-0004). It is asked once per Workspace, so no
// leaves everything unchanged for the rest of the session.
func (m *Model) offerInclude(inc *IncludeError) {
	m.askInclude(inc, func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.AddInclude(); err != nil {
			return m.setStatus("add include: "+err.Error(), true)
		}
		return m.reload("", fmt.Sprintf("added %q to %s", inc.Line, inc.Config))
	})
}

// deleteConnection asks before deleting the target Connection: its Host
// block when sshctl manages it, and its sshctl metadata.
func (m *Model) deleteConnection() tea.Cmd {
	c := m.moveTarget()
	if c == nil {
		return m.setStatus("no connection to delete", false)
	}
	if c.Open() {
		return m.setStatus("close "+c.Alias+"'s tabs before deleting it", true)
	}
	question := fmt.Sprintf("Delete %s from ssh_config?", c.Alias)
	if !c.Managed {
		if c.Description == "" && c.Group == "" && len(c.Tunnels) == 0 {
			return m.setStatus(c.Alias+" is defined outside sshctl; edit your ssh_config to remove it", true)
		}
		question = fmt.Sprintf("Remove %s's description, group and tunnels? Its Host block is not sshctl's.", c.Alias)
	}
	alias := c.Alias
	m.ask(question, func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.Delete(alias); err != nil {
			return m.setStatus("delete "+alias+": "+err.Error(), true)
		}
		return m.reload("", "deleted "+alias)
	})
	return nil
}

// render draws the connection form in a w×h pane. Under the Space field it
// lists spaces, the Spaces by number; under the group field the stored
// groups of the form's Space by Group number, since the form covers their
// headers. An alias equal to one of commands, sshctl's built-in commands,
// gets a warning under the alias field: it is reached only with sshctl -c
// (ADR-0011), but it is not refused.
func (f *connForm) render(w, h int, spaces, groups []string, bootstrapOn bool, commands []string) []string {
	lines := []string{"", "  " + boldStyle.Render(f.title()), ""}
	for i, fl := range f.fields {
		pointer := "  "
		value := fl.value.String()
		if i == f.focus {
			pointer = "› "
			value = fl.value.withCursor()
		} else if fl.value.empty() && fl.placeholder != "" {
			value = dimStyle.Render(fl.placeholder)
		}
		lines = append(lines, fmt.Sprintf("  %s%-14s %s", pointer, fl.label, value))
		if alias := strings.TrimSpace(fl.value.String()); fl.id == fieldAlias && slices.Contains(commands, alias) {
			lines = append(lines, "    "+errStyle.Render(fmt.Sprintf("sshctl %s is a command; use sshctl -c %s", alias, alias)))
		}
		if fl.id == fieldSpace {
			lines = append(lines, strings.Repeat(" ", 19)+dimStyle.Render(strings.Join(spaces, " · ")))
		}
		if fl.id == fieldGroup && len(groups) > 0 {
			numbers := []string{"0 none"}
			for n, g := range groups {
				numbers = append(numbers, fmt.Sprintf("%d %s", n+1, g))
			}
			lines = append(lines, strings.Repeat(" ", 19)+dimStyle.Render(strings.Join(numbers, " · ")))
		}
	}
	lines = append(lines, "")
	if !f.managed {
		lines = append(lines, "  "+dimStyle.Render("Host details stay in your own ssh_config."))
	}
	if f.err != "" {
		lines = append(lines, "  "+errStyle.Render(f.err))
	}
	hints := f.hints()
	lines = append(lines, "  "+dimStyle.Render(hints[0].String()))
	if f.old == "" {
		hint := hints[1].String()
		if !bootstrapOn {
			hint += " (experimental, off)"
		}
		lines = append(lines, "  "+dimStyle.Render(hint))
	}
	out := make([]string, 0, h)
	for _, l := range lines {
		if len(out) < h {
			out = append(out, fit(l, w))
		}
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}
