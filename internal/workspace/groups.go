package workspace

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// headerTarget returns the Group whose header is under the cursor, when the
// add, edit and delete keys act on it: the list has focus (or no Connection
// is active) and the header is a Group's, not the ungrouped section's.
func (m *Model) headerTarget() (string, bool) {
	if m.focus == focusPane && m.st.Active() != nil {
		return "", false
	}
	r, ok := m.st.CursorRow()
	if !ok || !r.Header() || r.Group == "" {
		return "", false
	}
	return r.Group, true
}

// groupInput is the status-line prompt for a new Group's name, or a Group's
// new name. A name the catalog refuses keeps it open with the reason.
type groupInput struct {
	prefixGate
	// old is the Group being renamed, or "" for a new Group.
	old  string
	name line
	err  string
}

func (in *groupInput) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := in.intercept(m, msg); ok {
		return cmd
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.modal = nil
	case "enter":
		return m.saveGroup(in)
	default:
		in.name.key(msg)
	}
	return nil
}

func (in *groupInput) paste(text string) { in.name.paste(text) }

func (in *groupInput) status() (string, bool) {
	prompt, help := "new group: ", "Enter add · Esc cancel"
	if in.old != "" {
		prompt, help = "rename group "+in.old+" to: ", "Enter rename · Esc cancel"
	}
	switch {
	case in.swallowed:
		help = "group prompt open: Esc to cancel"
	case in.err != "":
		help = in.err
	}
	return prompt + in.name.withCursor() + "  " + help, true
}

// startRenameGroup opens the prompt for group's new name.
func (m *Model) startRenameGroup(group string) tea.Cmd {
	m.openModal(&groupInput{old: group, name: newLine(group)})
	return nil
}

// saveGroup adds or renames the Group in, and puts the cursor on its header.
func (m *Model) saveGroup(in *groupInput) tea.Cmd {
	name := strings.TrimSpace(in.name.String())
	if in.old != "" && name == in.old {
		m.modal = nil
		return nil
	}
	var err error
	status := "added group " + name
	if in.old == "" {
		err = m.opts.Catalog.AddGroup(m.st.Space(), name)
	} else {
		err = m.opts.Catalog.RenameGroup(m.st.Space(), in.old, name)
		status = fmt.Sprintf("renamed group %s to %s", in.old, name)
	}
	if err != nil {
		in.err = err.Error()
		return nil
	}
	m.modal = nil
	if in.old != "" && m.st.Collapsed(in.old) {
		// The catalog keeps a renamed Group's collapsed state.
		m.st.SetCollapsed(in.old, false)
		m.st.SetCollapsed(name, true)
	}
	cmd := m.reload("", status)
	m.st.SetCursorGroup(name)
	return cmd
}

// askCreateGroup asks before creating the Group name, then runs then. When
// the catalog refuses the name, the reason is the status, or the error of
// the form the question was asked from.
func (m *Model) askCreateGroup(name string, then func(*Model) tea.Cmd) {
	m.ask(fmt.Sprintf("Create group %s? (y/n)", name), func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.AddGroup(m.st.Space(), name); err != nil {
			if f := m.form(); f != nil {
				f.err = err.Error()
				return nil
			}
			return m.setStatus("create group "+name+": "+err.Error(), true)
		}
		return then(m)
	})
}

// deleteGroup asks before deleting group, saying how many Connections
// become ungrouped.
func (m *Model) deleteGroup(group string) tea.Cmd {
	n := len(m.st.Members(group))
	verb := "become"
	if n == 1 {
		verb = "becomes"
	}
	question := fmt.Sprintf("Delete group %s? %s %s ungrouped. (y/n)", group, countConnections(n), verb)
	m.ask(question, func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.DeleteGroup(m.st.Space(), group); err != nil {
			return m.setStatus("delete group "+group+": "+err.Error(), true)
		}
		m.st.SetCollapsed(group, false) // the catalog no longer lists it as collapsed
		return m.reload("", "deleted group "+group)
	})
	return nil
}
