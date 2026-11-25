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
	return in.promptKey(m, msg, in.keys(), &in.name)
}

func (in *groupInput) keys() []boundKey {
	done := "add"
	if in.old != "" {
		done = "rename"
	}
	return promptKeys(done, func(m *Model) tea.Cmd { return m.saveGroup(in) }, nil)
}

func (in *groupInput) hints() []hintLine { return []hintLine{in.promptHints(in.keys())} }

func (in *groupInput) paste(text string) { in.name.paste(text) }

func (in *groupInput) status() (string, bool) {
	prompt := "new group: "
	if in.old != "" {
		prompt = "rename group " + in.old + " to: "
	}
	help := in.promptHints(in.keys()).String()
	if in.err != "" && !in.swallowed {
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
		m.showModal(nil)
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
	m.showModal(nil)
	if in.old != "" && m.st.Collapsed(in.old) {
		// The catalog keeps a renamed Group's collapsed state.
		m.st.SetCollapsed(in.old, false)
		m.st.SetCollapsed(name, true)
	}
	cmd := m.reload("", status)
	m.st.SetCursorGroup(name)
	return cmd
}

// askCreateGroup asks before creating the Group name in the Space space
// ("" for the default Space), then runs then. When the catalog refuses the
// name, the reason is the status, or the error of the form the question was
// asked from.
func (m *Model) askCreateGroup(space, name string, then func(*Model) tea.Cmd) {
	m.ask(fmt.Sprintf("Create group %s?", name), func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.AddGroup(space, name); err != nil {
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
	question := fmt.Sprintf("Delete group %s? %s %s ungrouped.", group, plural(n, "connection"), verb)
	m.ask(question, func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.DeleteGroup(m.st.Space(), group); err != nil {
			return m.setStatus("delete group "+group+": "+err.Error(), true)
		}
		m.st.SetCollapsed(group, false) // the catalog no longer lists it as collapsed
		return m.reload("", "deleted group "+group)
	})
	return nil
}
