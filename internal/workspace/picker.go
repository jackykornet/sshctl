package workspace

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// spacePicker lists every Space in the pane, with a cursor. Enter switches
// to the Space under it; it is the only way to reach Spaces after the 9th.
type spacePicker struct {
	prefixGate
	cursor int // index into State.SpaceTabs
	// note is what the last action said, such as why a delete was refused,
	// shown on the status line until the next key.
	note    string
	noteErr bool
}

// openPicker opens the Space picker with its cursor on the active Space.
func (m *Model) openPicker() {
	p := &spacePicker{}
	for i, t := range m.st.SpaceTabs() {
		if t.Active {
			p.cursor = i
		}
	}
	m.openModal(p)
}

// picker returns the Space picker when it is the modal, or when it waits
// behind a prompt or confirmation it opened.
func (m *Model) picker() *spacePicker {
	md := m.modal
	if c, ok := md.(*confirmation); ok {
		md = c.back
	}
	if in, ok := md.(*spaceInput); ok {
		return in.back
	}
	p, _ := md.(*spacePicker)
	return p
}

func (p *spacePicker) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := p.intercept(m, msg); ok {
		return cmd
	}
	p.note, p.noteErr = "", false
	n := len(m.st.SpaceTabs())
	switch msg.String() {
	case "esc", "ctrl+c", "q", "P":
		m.modal = nil
	case "up", "k":
		p.cursor--
	case "down", "j":
		p.cursor++
	case "a":
		m.openModal(&spaceInput{back: p})
		return nil
	case "e":
		if t, ok := p.target(m); ok {
			m.openModal(&spaceInput{back: p, old: t.Name, name: newLine(t.Name)})
		}
		return nil
	case "d":
		return m.deleteSpace(p)
	case "shift+up", "K":
		return m.shiftSpace(p, -1)
	case "shift+down", "J":
		return m.shiftSpace(p, 1)
	case "enter", "space":
		m.modal = nil
		ref, _ := m.st.SpaceRef(p.cursor + 1)
		return m.showSpace(ref)
	}
	p.cursor = max(0, min(p.cursor, n-1))
	return nil
}

// target is the Space under the cursor.
func (p *spacePicker) target(m *Model) (SpaceTab, bool) {
	tabs := m.st.SpaceTabs()
	if p.cursor < 0 || p.cursor >= len(tabs) {
		return SpaceTab{}, false
	}
	return tabs[p.cursor], true
}

func (p *spacePicker) paste(string) {}

func (p *spacePicker) status() (string, bool) {
	switch {
	case p.swallowed:
		return "space picker open: Esc to close", true
	case p.noteErr:
		return errStyle.Render(p.note), true
	case p.note != "":
		return p.note, true
	}
	return "Enter switch · a add · e rename · d delete · Shift+↑/↓ move · Esc close", true
}

// shiftSpace moves the Space under the picker's cursor delta places up or
// down, the cursor following it. It does nothing at either end.
func (m *Model) shiftSpace(p *spacePicker, delta int) tea.Cmd {
	t, ok := p.target(m)
	to := p.cursor + delta
	if !ok || to < 0 || to >= len(m.st.SpaceTabs()) {
		return nil
	}
	if err := m.opts.Catalog.MoveSpace(t.Name, to+1); err != nil {
		p.say("move space "+t.Name+": "+err.Error(), true)
		return nil
	}
	cmd := m.reload("", "")
	p.setCursor(m, t.Name)
	return cmd
}

// say shows text on the picker's status line until the next key.
func (p *spacePicker) say(text string, isErr bool) { p.note, p.noteErr = text, isErr }

// deleteSpace deletes the Space under the picker's cursor, asking first
// when it looks empty. The catalog refuses the default Space and one that
// still has Groups or Connections, naming what is left; a Space that is
// not empty is given to it without asking, so its refusal shows at once.
func (m *Model) deleteSpace(p *spacePicker) tea.Cmd {
	t, ok := p.target(m)
	if !ok {
		return nil
	}
	del := func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.DeleteSpace(t.Name); err != nil {
			p.say("delete space "+t.Name+": "+err.Error(), true)
			return nil
		}
		cmd := m.reload("", "")
		p.cursor = min(p.cursor, len(m.st.SpaceTabs())-1)
		p.say("deleted space "+t.Name, false)
		return cmd
	}
	if ref, _ := m.st.SpaceRef(p.cursor + 1); ref == "" || !m.st.SpaceEmpty(ref) {
		return del(m)
	}
	m.ask(fmt.Sprintf("Delete space %s? (y/n)", t.Name), del)
	return nil
}

// render draws the picker in the w by h pane.
func (p *spacePicker) render(m *Model, w, h int) []string {
	lines := []string{fit(" "+boldStyle.Render("Spaces"), w)}
	for i, t := range m.st.SpaceTabs() {
		pointer := "  "
		if i == p.cursor {
			pointer = " ›"
		}
		name := t.Name
		switch {
		case t.Active:
			name = spaceActiveStyle.Render(name)
		case !t.Live:
			name = dimStyle.Render(name)
		}
		lines = append(lines, fit(fmt.Sprintf("%s %s %s", pointer, dimStyle.Render(fmt.Sprint(i+1)), name), w))
	}
	lines = lines[:min(len(lines), h)]
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return lines
}

// spaceInput is the status-line prompt for a new Space's name, or a Space's
// new name, asked from the picker. A name the catalog refuses keeps it open
// with the reason; Esc returns to the picker.
type spaceInput struct {
	prefixGate
	back *spacePicker
	// old is the name of the Space being renamed, or "" for a new Space.
	old  string
	name line
	err  string
}

func (in *spaceInput) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := in.intercept(m, msg); ok {
		return cmd
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.modal = in.back
	case "enter":
		return m.saveSpace(in)
	default:
		in.name.key(msg)
	}
	return nil
}

func (in *spaceInput) paste(text string) { in.name.paste(text) }

func (in *spaceInput) status() (string, bool) {
	prompt, help := "new space: ", "Enter add · Esc cancel"
	if in.old != "" {
		prompt, help = "rename space "+in.old+" to: ", "Enter rename · Esc cancel"
	}
	switch {
	case in.swallowed:
		help = "space prompt open: Esc to cancel"
	case in.err != "":
		help = in.err
	}
	return prompt + in.name.withCursor() + "  " + help, true
}

// saveSpace adds or renames the Space in, then returns to the picker with
// its cursor on that Space.
func (m *Model) saveSpace(in *spaceInput) tea.Cmd {
	name := strings.TrimSpace(in.name.String())
	if in.old != "" && name == in.old {
		m.modal = in.back
		return nil
	}
	var err error
	status := "added space " + name
	if in.old == "" {
		err = m.opts.Catalog.AddSpace(name)
	} else {
		err = m.opts.Catalog.RenameSpace(in.old, name)
		status = fmt.Sprintf("renamed space %s to %s", in.old, name)
	}
	if err != nil {
		in.err = err.Error()
		return nil
	}
	if in.old != "" {
		m.st.RenameSpace(in.old, name)
	}
	m.modal = in.back
	cmd := m.reload("", "")
	in.back.setCursor(m, name)
	in.back.say(status, false)
	return cmd
}

// setCursor puts the picker's cursor on the Space name.
func (p *spacePicker) setCursor(m *Model, name string) {
	for i, t := range m.st.SpaceTabs() {
		if t.Name == name {
			p.cursor = i
		}
	}
}
