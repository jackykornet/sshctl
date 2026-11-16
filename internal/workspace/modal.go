package workspace

import (
	"fmt"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// A modal is the prompt, form or screen that takes the Workspace's input
// before anything else. The Workspace holds at most one (Model.modal). While
// it is open, key presses and paste go only to it, mouse input is ignored,
// and it may claim the status line. Nothing behind it sees any of this.
//
// What each modal lets through:
//   - confirmation: every key answers it (y or Y is yes, anything else no).
//   - helpScreen: every key closes it.
//   - connForm, moveInput and groupInput: they edit text, so the prefix key arms a
//     prefix of their own (prefixGate). Only prefix q acts, quitting the
//     Workspace (after asking, when Sessions are live, and returning to the
//     modal on no). Every other prefix command is swallowed: it would act
//     on the Workspace hidden behind the modal, and opening help would
//     replace the modal. The status line then says how to leave the modal.
type modal interface {
	key(m *Model, msg tea.KeyPressMsg) tea.Cmd
	paste(text string)
	// status returns the status-line text, or false to leave the status
	// line to the Workspace.
	status() (string, bool)
	// prefixArmed reports whether the modal is waiting for a prefix command.
	prefixArmed() bool
}

// openModal makes md the Workspace's modal. The Workspace's own prefix and
// any drag in progress end, since md now gets all input.
func (m *Model) openModal(md modal) {
	m.prefix = false
	if m.drag != nil {
		m.drag = nil
		m.st.ShowUngrouped(false)
	}
	m.modal = md
}

// form returns the connection form when it is the modal, or when it waits
// behind the confirmation it opened.
func (m *Model) form() *connForm {
	md := m.modal
	if c, ok := md.(*confirmation); ok {
		md = c.back
	}
	f, _ := md.(*connForm)
	return f
}

// confirm returns the open confirmation, or nil.
func (m *Model) confirm() *confirmation {
	c, _ := m.modal.(*confirmation)
	return c
}

// confirmation is a y/n question on the status line.
type confirmation struct {
	question string
	yes      func(*Model) tea.Cmd
	// back is the modal the question was asked from; it returns once the
	// question is answered.
	back modal
}

// ask opens a y/n question, returning to the current modal once answered.
func (m *Model) ask(question string, yes func(*Model) tea.Cmd) {
	back := m.modal
	m.openModal(&confirmation{question: question, yes: yes, back: back})
}

func (c *confirmation) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	m.modal = c.back
	if k := msg.String(); k == "y" || k == "Y" {
		return c.yes(m)
	}
	return nil
}

func (c *confirmation) paste(string)           {}
func (c *confirmation) status() (string, bool) { return c.question, true }
func (c *confirmation) prefixArmed() bool      { return false }

// helpScreen lists the keys over the whole Workspace.
type helpScreen struct{}

func (helpScreen) key(m *Model, _ tea.KeyPressMsg) tea.Cmd {
	m.modal = nil
	return nil
}

func (helpScreen) paste(string)           {}
func (helpScreen) status() (string, bool) { return "", false }
func (helpScreen) prefixArmed() bool      { return false }

// prefixGate is the prefix inside a modal that edits text; see modal.
type prefixGate struct {
	armed bool
	// swallowed is set from a swallowed prefix command until the next key,
	// so the modal's status line can say how to leave it.
	swallowed bool
}

// intercept handles msg when it is the prefix key or the key after it.
func (g *prefixGate) intercept(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	g.swallowed = false
	switch {
	case g.armed:
		g.armed = false
		if k == "q" {
			return m.quit(), true
		}
		g.swallowed = true
		return nil, true
	case k == m.opts.PrefixKey:
		g.armed = true
		return nil, true
	}
	return nil, false
}

func (g *prefixGate) prefixArmed() bool { return g.armed }

// moveInput is the status-line prompt asking which group to move alias to.
type moveInput struct {
	prefixGate
	alias string
	group []rune
}

func (in *moveInput) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := in.intercept(m, msg); ok {
		return cmd
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.modal = nil
	case "enter":
		m.modal = nil
		return m.moveTo(in.alias, string(in.group))
	default:
		in.group, _ = editLine(in.group, msg)
	}
	return nil
}

func (in *moveInput) paste(text string) { in.group = pasteLine(in.group, text) }

func (in *moveInput) status() (string, bool) {
	help := "Enter move · empty for none · Esc cancel"
	if in.swallowed {
		help = "move prompt open: Esc to cancel"
	}
	return fmt.Sprintf("move %s to group: %s█  %s", in.alias, string(in.group), help), true
}

// editLine applies a line-editing key to value: backspace deletes the last
// character, ctrl+u clears the line, and printable text is appended. It
// reports false, leaving value as it was, for any other key.
func editLine(value []rune, msg tea.KeyPressMsg) ([]rune, bool) {
	switch msg.String() {
	case "backspace":
		if len(value) > 0 {
			value = value[:len(value)-1]
		}
		return value, true
	case "ctrl+u":
		return nil, true
	}
	if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
		return append(value, []rune(msg.Text)...), true
	}
	return value, false
}

// pasteLine appends pasted text to value, dropping line breaks and other
// control characters, since the line holds a single line.
func pasteLine(value []rune, text string) []rune {
	for _, r := range text {
		if !unicode.IsControl(r) {
			value = append(value, r)
		}
	}
	return value
}
