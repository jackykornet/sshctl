package workspace

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Host is how OpenSSH reaches a Connection whose Host block sshctl manages.
type Host struct {
	HostName     string
	User         string
	Port         string
	IdentityFile string
}

// Spec is a Connection as entered in the connection form.
type Spec struct {
	Alias       string
	Description string
	Group       string
	// Managed is true when sshctl writes the Host block, so Host applies.
	// For a Connection defined elsewhere in ssh_config only the description
	// and group are saved.
	Managed bool
	Host    Host
}

// IncludeError is returned by Options.SaveConnection when the user's
// ssh_config does not yet read sshctl's hosts file. Saving can be retried
// after Options.AddInclude adds Line to Config.
type IncludeError struct {
	Line   string
	Config string
}

func (e *IncludeError) Error() string { return e.Config + " does not contain " + e.Line }

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
	value       []rune
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
			{id: fieldDescription, label: "Description", value: []rune(s.Description)},
			{id: fieldGroup, label: "Group", placeholder: "none", value: []rune(s.Group)},
		}
	}
	return f
}

func managedFields(s Spec) []formField {
	return []formField{
		{id: fieldAlias, label: "Alias", placeholder: "name used with ssh", value: []rune(s.Alias)},
		{id: fieldHostName, label: "HostName", placeholder: "host or IP address", value: []rune(s.Host.HostName)},
		{id: fieldUser, label: "User", placeholder: "OpenSSH default", value: []rune(s.Host.User)},
		{id: fieldPort, label: "Port", placeholder: "22", value: []rune(s.Host.Port)},
		{id: fieldIdentity, label: "Identity file", placeholder: "OpenSSH default", value: []rune(s.Host.IdentityFile)},
		{id: fieldDescription, label: "Description", value: []rune(s.Description)},
		{id: fieldGroup, label: "Group", placeholder: "none", value: []rune(s.Group)},
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
		v := strings.TrimSpace(string(fl.value))
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
	if m.opts.SaveConnection == nil {
		return nil
	}
	group := ""
	if r, ok := m.st.CursorRow(); ok {
		group = r.Group
	}
	m.openModal(newAddForm(group))
	return nil
}

func (m *Model) startEdit() tea.Cmd {
	if m.opts.SaveConnection == nil {
		return nil
	}
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
		f.focus = (f.focus + 1) % len(f.fields)
	case "shift+tab", "up":
		f.focus = (f.focus + len(f.fields) - 1) % len(f.fields)
	case "enter":
		if f.focus < len(f.fields)-1 {
			f.focus++
			return nil
		}
		return m.saveForm(f)
	case "ctrl+s":
		return m.saveForm(f)
	default:
		fl.value, _ = editLine(fl.value, msg)
	}
	return nil
}

func (f *connForm) paste(text string) {
	fl := &f.fields[f.focus]
	fl.value = pasteLine(fl.value, text)
}

func (f *connForm) status() (string, bool) { return "", false }

// saveForm saves the form through Options.SaveConnection and reloads the
// list. Errors stay in the form so they can be corrected; when ssh_config
// must Include sshctl's hosts file first, it asks before adding the line.
func (m *Model) saveForm(f *connForm) tea.Cmd {
	s := f.spec()
	if f.old != "" && s.Alias != f.old {
		if c := m.st.Get(f.old); c != nil && c.Open() {
			f.err = "close " + f.old + "'s tabs before renaming it"
			return nil
		}
	}
	err := m.opts.SaveConnection(f.old, s)
	var inc *IncludeError
	if errors.As(err, &inc) && m.opts.AddInclude != nil {
		m.ask(fmt.Sprintf("Add %q to the top of %s? (y/n)", inc.Line, inc.Config), func(m *Model) tea.Cmd {
			if err := m.opts.AddInclude(); err != nil {
				f.err = err.Error()
				return nil
			}
			return m.saveForm(f)
		})
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
	return m.reload(s.Alias, fmt.Sprintf("%s %s", verb, s.Alias))
}

// deleteConnection asks before deleting the target Connection: its Host
// block when sshctl manages it, and its sshctl metadata.
func (m *Model) deleteConnection() tea.Cmd {
	if m.opts.DeleteConnection == nil {
		return nil
	}
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
		if err := m.opts.DeleteConnection(alias); err != nil {
			return m.setStatus("delete "+alias+": "+err.Error(), true)
		}
		return m.reload("", "deleted "+alias)
	})
	return nil
}

// renderForm draws the connection form in a w×h pane.
func (f *connForm) render(w, h int) []string {
	lines := []string{"", "  " + boldStyle.Render(f.title()), ""}
	for i, fl := range f.fields {
		pointer := "  "
		value := string(fl.value)
		if i == f.focus {
			pointer = "› "
			value += "█"
		} else if value == "" && fl.placeholder != "" {
			value = dimStyle.Render(fl.placeholder)
		}
		lines = append(lines, fmt.Sprintf("  %s%-14s %s", pointer, fl.label, value))
	}
	lines = append(lines, "")
	if !f.managed {
		lines = append(lines, "  "+dimStyle.Render("Host details stay in your own ssh_config."))
	}
	if f.err != "" {
		lines = append(lines, "  "+errStyle.Render(f.err))
	}
	lines = append(lines, "  "+dimStyle.Render("Tab/↑↓ move · Ctrl+S save · Esc cancel"))
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
