package workspace

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// A modal is the prompt, form or screen that takes the Workspace's input
// before anything else. The Workspace holds at most one, in modal mode. While
// it is open, key presses and paste go only to it, mouse input is ignored,
// and it may claim the status line. Nothing behind it sees any of this.
//
// What each modal lets through:
//   - confirmation: every key answers it (y or Y is yes, anything else no).
//   - helpScreen: ↑/↓ and PgUp/PgDn scroll it; every other key closes it.
//   - spacePicker: its own keys (see its status line); Esc, q or P close it.
//     It opens spaceInput and confirmations, which return to it.
//   - connForm, moveInput, groupInput, spaceInput and spacePicker: the prefix key arms a
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
	m.showModal(md)
}

// form returns the connection form when it is the modal, or when it waits
// behind the confirmation it opened.
func (m *Model) form() *connForm {
	md := m.mode.modal
	if c, ok := md.(*confirmation); ok {
		md = c.back
	}
	f, _ := md.(*connForm)
	return f
}

// confirm returns the open confirmation, or nil.
func (m *Model) confirm() *confirmation {
	c, _ := m.mode.modal.(*confirmation)
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
	back := m.mode.modal
	m.openModal(&confirmation{question: question, yes: yes, back: back})
}

// key answers the question: y or Y is yes, any other key no.
func (c *confirmation) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	m.showModal(c.back)
	if bk, ok := findKey(c.keys(), msg.String()); ok {
		return bk.run(m, msg)
	}
	return nil
}

func (c *confirmation) keys() []boundKey {
	yes := func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return c.yes(m) }
	no := func(*Model, tea.KeyPressMsg) tea.Cmd { return nil }
	return []boundKey{on("y", yes).as("yes"), on("Y", yes), on("n", no).as("no")}
}

func (c *confirmation) hints() []hintLine { return []hintLine{hintsFor(c.keys(), "y", "n")} }

func (c *confirmation) paste(string) {}
func (c *confirmation) status() (string, bool) {
	return c.question + "  " + c.hints()[0].String(), true
}
func (c *confirmation) prefixArmed() bool { return false }

// helpScreen lists the keys over the whole Workspace, scrolled down by top
// lines when they do not fit.
type helpScreen struct{ top int }

// key scrolls the help screen; any key not in its table closes it.
func (h *helpScreen) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if bk, ok := findKey(h.keys(), msg.String()); ok {
		return bk.run(m, msg)
	}
	m.showModal(nil)
	return nil
}

func (h *helpScreen) keys() []boundKey {
	by := func(pages, lines int) action {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			h.top += pages*max(1, m.helpRows()-1) + lines
			h.top = max(0, min(h.top, m.helpMaxTop()))
			return nil
		}
	}
	return []boundKey{
		on("up", by(0, -1)).as("scroll"),
		on("down", by(0, 1)).as("scroll"),
		on("pgup", by(-1, 0)).as("scroll"),
		on("pgdown", by(1, 0)).as("scroll"),
		on("esc", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.showModal(nil); return nil }).as("close"),
	}
}

func (h *helpScreen) hints() []hintLine {
	return []hintLine{hintsFor(h.keys(), "esc", "up", "down", "pgup", "pgdown")}
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
	group line
}

func (in *moveInput) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	return in.promptKey(m, msg, in.keys(), &in.group)
}

func (in *moveInput) keys() []boundKey {
	return promptKeys("move", func(m *Model) tea.Cmd {
		m.showModal(nil)
		return m.moveTo(in.alias, in.group.String())
	}, nil)
}

func (in *moveInput) hints() []hintLine { return []hintLine{in.promptHints(in.keys())} }

func (in *moveInput) paste(text string) { in.group.paste(text) }

func (in *moveInput) status() (string, bool) {
	return fmt.Sprintf("move %s to group: %s  %s  (0 or empty for none)", in.alias, in.group.withCursor(), in.promptHints(in.keys())), true
}

// promptKeys are the keys of a status-line prompt: Enter runs done, named
// doneName, and Esc or Ctrl+C go back to back (nil closes the prompt).
func promptKeys(doneName string, done func(*Model) tea.Cmd, back modal) []boundKey {
	cancel := func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.showModal(back); return nil }
	return []boundKey{
		on("enter", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return done(m) }).as(doneName),
		on("esc", cancel).as("cancel"),
		on("ctrl+c", cancel),
	}
}

// promptKey runs msg from keys, or else types it into text.
func (g *prefixGate) promptKey(m *Model, msg tea.KeyPressMsg, keys []boundKey, text *line) tea.Cmd {
	if cmd, ok := g.intercept(m, msg); ok {
		return cmd
	}
	if bk, ok := findKey(keys, msg.String()); ok {
		return bk.run(m, msg)
	}
	text.key(msg)
	return nil
}

// promptHints is a prompt's hint line: Enter and Esc, or only Esc after a
// swallowed prefix command, to say how to leave.
func (g *prefixGate) promptHints(keys []boundKey) hintLine {
	if g.swallowed {
		return hintsFor(keys, "esc")
	}
	return hintsFor(keys, "enter", "esc")
}
