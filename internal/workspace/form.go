package workspace

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
)

// Host is how OpenSSH reaches a Connection whose Host block sshctl manages.
type Host = catalog.Host

// Spec is a Connection as entered in the connection form.
type Spec = catalog.Spec

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
	fieldGroup
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
	old     string
	managed bool
	fields  []formField
	focus   int
	err     string
}

func newAddForm(group string) *connForm {
	return &connForm{managed: true, fields: managedFields(Spec{Group: group})}
}

func newEditForm(c *Connection) *connForm {
	s := Spec{Alias: c.Alias, Description: c.Description, Group: c.Group, Managed: c.Managed, Host: c.Host}
	f := &connForm{old: c.Alias, managed: c.Managed}
	if c.Managed {
		f.fields = managedFields(s)
	} else {
		f.fields = []formField{
			{id: fieldDescription, label: "Description", value: newLine(s.Description)},
			{id: fieldGroup, label: "Group", placeholder: "none", value: newLine(s.Group)},
		}
	}
	return f
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
	}
}

func (f *connForm) title() string {
	switch {
	case f.old == "":
		return "Add connection"
	case f.managed:
		return "Edit " + f.old
	default:
		return "Edit " + f.old + " (description and group)"
	}
}

// spec returns the form's values with surrounding spaces trimmed.
func (f *connForm) spec() Spec {
	s := Spec{Alias: f.old, Managed: f.managed}
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

func (m *Model) startAdd() tea.Cmd {
	group := ""
	if r, ok := m.st.CursorRow(); ok {
		group = r.Group
	}
	m.openModal(newAddForm(group))
	return nil
}

func (m *Model) startEdit() tea.Cmd {
	c := m.moveTarget()
	if c == nil {
		return m.setStatus("no connection to edit", false)
	}
	m.openModal(newEditForm(c))
	return nil
}

func (f *connForm) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := f.intercept(m, msg); ok {
		return cmd
	}
	fl := &f.fields[f.focus]
	switch msg.String() {
	case "esc":
		m.modal = nil
	case "tab", "down":
		f.focusField(f.focus + 1)
	case "shift+tab", "up":
		f.focusField(f.focus - 1)
	case "enter":
		if f.focus < len(f.fields)-1 {
			f.focusField(f.focus + 1)
			return nil
		}
		return m.saveForm(f, false)
	case "ctrl+s":
		return m.saveForm(f, false)
	case "ctrl+o":
		if f.old == "" {
			return m.saveForm(f, true)
		}
	default:
		fl.value.key(msg)
	}
	return nil
}

// focusField moves the focus to field i, wrapping around, with the cursor
// at the end of its value.
func (f *connForm) focusField(i int) {
	f.focus = (i + len(f.fields)) % len(f.fields)
	f.fields[f.focus].value.end()
}

func (f *connForm) paste(text string) { f.fields[f.focus].value.paste(text) }

func (f *connForm) status() (string, bool) {
	if f.swallowed {
		return "form open: Esc to cancel", true
	}
	return "", false
}

// saveForm saves the form through the Catalog and reloads the
// list. Errors stay in the form so they can be corrected; when ssh_config
// must Include sshctl's hosts file first, it asks before adding the line.
// With bootstrap, a successful save then opens a Bootstrap tab for the
// saved alias.
func (m *Model) saveForm(f *connForm, bootstrap bool) tea.Cmd {
	s := f.spec()
	group, ok := m.st.ResolveGroup(s.Group)
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
	err := m.opts.Catalog.Save(f.old, s)
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
		m.askCreateGroup(s.Group, func(m *Model) tea.Cmd { return m.saveForm(f, bootstrap) })
		return nil
	}
	if err != nil {
		f.err = err.Error()
		return nil
	}
	m.modal = nil
	verb := "saved"
	if f.old == "" {
		verb = "added"
	}
	cmd := m.reload(s.Alias, fmt.Sprintf("%s %s", verb, s.Alias))
	if bootstrap {
		return tea.Batch(cmd, m.open(s.Alias, Launch{Action: ActionBootstrap}))
	}
	return cmd
}

// askInclude asks before adding inc's Include line to the user's ssh_config
// (ADR-0002); yes runs on y.
func (m *Model) askInclude(inc *IncludeError, yes func(*Model) tea.Cmd) {
	m.ask(fmt.Sprintf("Add %q to the top of %s? (y/n)", inc.Line, inc.Config), yes)
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
	question := fmt.Sprintf("Delete %s from ssh_config? (y/n)", c.Alias)
	if !c.Managed {
		if c.Description == "" && c.Group == "" {
			return m.setStatus(c.Alias+" is defined outside sshctl; edit your ssh_config to remove it", true)
		}
		question = fmt.Sprintf("Remove %s's description and group? Its Host block is not sshctl's. (y/n)", c.Alias)
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

// render draws the connection form in a w×h pane. Under the group field
// it lists the stored groups by Group number, since the form covers their
// headers.
func (f *connForm) render(w, h int, groups []string) []string {
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
	lines = append(lines, "  "+dimStyle.Render("Tab/↑↓ move · Ctrl+S save · Esc cancel"))
	if f.old == "" {
		lines = append(lines, "  "+dimStyle.Render("Ctrl+O save and Bootstrap"))
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
