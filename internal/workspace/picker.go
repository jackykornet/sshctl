package workspace

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
)

// spacePicker lists every Space number in the pane, one row each with its
// Space or free, then any Space without a number, with a cursor. Enter
// switches to the Space under it; it is the only way to reach a Space
// without a number.
type spacePicker struct {
	prefixGate
	cursor int // index into State.SpaceSlots
	// note is what the last action said, such as why a delete was refused,
	// shown on the status line until the next key.
	note    string
	noteErr bool
}

// openPicker opens the Space picker with its cursor on the active Space.
func (m *Model) openPicker() {
	p := &spacePicker{}
	for i, t := range m.st.SpaceSlots() {
		if t.Active {
			p.cursor = i
		}
	}
	m.openModal(p)
}

// picker returns the Space picker when it is the modal, or when it waits
// behind a prompt or confirmation it opened.
func (m *Model) picker() *spacePicker {
	md := m.mode.modal
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
	if bk, ok := findKey(p.keys(), msg.String()); ok {
		return bk.run(m, msg)
	}
	return nil
}

func (p *spacePicker) keys() []boundKey {
	move := func(delta int) action {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			p.cursor = max(0, min(p.cursor+delta, len(m.st.SpaceSlots())-1))
			return nil
		}
	}
	shift := func(delta int) action {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.shiftSpace(p, delta) }
	}
	enter := func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		t, ok := p.target(m)
		switch {
		case !ok:
			return nil
		case t.Free():
			p.say(fmt.Sprintf("no Space %d", t.Number), true)
			return nil
		}
		m.showModal(nil)
		return m.showSpace(t.Ref)
	}
	add := func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		in := &spaceInput{back: p}
		if t, ok := p.target(m); ok && t.Free() {
			in.number = t.Number
		}
		m.openModal(in)
		return nil
	}
	rename := func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if t, ok := p.target(m); ok && !t.Free() {
			m.openModal(&spaceInput{back: p, old: t.Name, name: newLine(t.Name)})
		}
		return nil
	}
	closePicker := func(m *Model, _ tea.KeyPressMsg) tea.Cmd { m.showModal(nil); return nil }
	return []boundKey{
		on("enter", enter).as("switch"), on("space", enter),
		on("a", add).as("add"),
		on("e", rename).as("rename"),
		on("d", func(m *Model, _ tea.KeyPressMsg) tea.Cmd { return m.deleteSpace(p) }).as("delete"),
		on("shift+up", shift(-1)).as("move"), on("K", shift(-1)),
		on("shift+down", shift(1)).as("move"), on("J", shift(1)),
		on("up", move(-1)), on("k", move(-1)),
		on("down", move(1)), on("j", move(1)),
		on("esc", closePicker).as("close"), on("ctrl+c", closePicker), on("q", closePicker), on("P", closePicker),
	}
}

func (p *spacePicker) hints() []hintLine {
	if p.swallowed {
		return []hintLine{hintsFor(p.keys(), "esc")}
	}
	return []hintLine{hintsFor(p.keys(), "enter", "a", "e", "d", "shift+up", "shift+down", "esc")}
}

// target is the row under the cursor: a Space, or a free Space number.
func (p *spacePicker) target(m *Model) (SpaceTab, bool) {
	tabs := m.st.SpaceSlots()
	if p.cursor < 0 || p.cursor >= len(tabs) {
		return SpaceTab{}, false
	}
	return tabs[p.cursor], true
}

func (p *spacePicker) paste(string) {}

func (p *spacePicker) status() (string, bool) {
	switch {
	case p.swallowed:
		return "space picker open  " + p.hints()[0].String(), true
	case p.noteErr:
		return errStyle.Render(p.note), true
	case p.note != "":
		return p.note, true
	}
	return p.hints()[0].String(), true
}

// shiftSpace moves the Space under the picker's cursor to the next Space
// number up (delta -1) or down (1), swapping with the Space that has it;
// the cursor follows it. It does nothing at 1 or 9, on a free number, or
// for a Space without a number.
func (m *Model) shiftSpace(p *spacePicker, delta int) tea.Cmd {
	t, ok := p.target(m)
	to := t.Number + delta
	if !ok || t.Free() || t.Number == 0 || to < 1 || to > catalog.MaxSpaces {
		return nil
	}
	if err := m.opts.Catalog.MoveSpace(t.Name, to); err != nil {
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
	if !ok || t.Free() {
		return nil
	}
	del := func(m *Model) tea.Cmd {
		if err := m.opts.Catalog.DeleteSpace(t.Name); err != nil {
			p.say("delete space "+t.Name+": "+err.Error(), true)
			return nil
		}
		cmd := m.reload("", "")
		p.cursor = min(p.cursor, len(m.st.SpaceSlots())-1) // a numbered row stays, free
		p.say("deleted space "+t.Name, false)
		return cmd
	}
	if t.Ref == "" || !m.st.SpaceEmpty(t.Ref) {
		return del(m)
	}
	m.ask(fmt.Sprintf("Delete space %s?", t.Name), del)
	return nil
}

// render draws the picker in the w by h pane: a row per Space number,
// [2] acme, or [3] empty dimmed when it is free, then each Space without a
// number.
func (p *spacePicker) render(m *Model, w, h int) []string {
	lines := []string{fit(" "+boldStyle.Render("Spaces"), w)}
	for i, t := range m.st.SpaceSlots() {
		pointer := "  "
		if i == p.cursor {
			pointer = " ›"
		}
		number := "   "
		if t.Number > 0 {
			number = fmt.Sprintf("[%d]", t.Number)
		}
		var row string
		switch {
		case t.Free():
			row = dimStyle.Render(number + " empty")
		case t.Active:
			row = dimStyle.Render(number) + " " + spaceActiveStyle.Render(t.Name)
		case !t.Live:
			row = dimStyle.Render(number + " " + t.Name)
		default:
			row = dimStyle.Render(number) + " " + t.Name
		}
		lines = append(lines, fit(pointer+" "+row, w))
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
	old string
	// number is the free Space number a new Space takes, or 0 for the
	// lowest free one.
	number int
	name   line
	err    string
}

func (in *spaceInput) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	return in.promptKey(m, msg, in.keys(), &in.name)
}

func (in *spaceInput) keys() []boundKey {
	done := "add"
	if in.old != "" {
		done = "rename"
	}
	return promptKeys(done, func(m *Model) tea.Cmd { return m.saveSpace(in) }, in.back)
}

func (in *spaceInput) hints() []hintLine { return []hintLine{in.promptHints(in.keys())} }

func (in *spaceInput) paste(text string) { in.name.paste(text) }

func (in *spaceInput) status() (string, bool) {
	prompt := "new space: "
	if in.number > 0 {
		prompt = fmt.Sprintf("new space %d: ", in.number)
	}
	if in.old != "" {
		prompt = "rename space " + in.old + " to: "
	}
	help := in.promptHints(in.keys()).String()
	if in.err != "" && !in.swallowed {
		help = in.err
	}
	return prompt + in.name.withCursor() + "  " + help, true
}

// saveSpace adds or renames the Space in, then returns to the picker with
// its cursor on that Space.
func (m *Model) saveSpace(in *spaceInput) tea.Cmd {
	name := strings.TrimSpace(in.name.String())
	if in.old != "" && name == in.old {
		m.showModal(in.back)
		return nil
	}
	var err error
	status := "added space " + name
	switch {
	case in.old == "" && in.number > 0:
		err = m.opts.Catalog.AddSpaceAt(name, in.number)
	case in.old == "":
		err = m.opts.Catalog.AddSpace(name)
	default:
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
	m.showModal(in.back)
	cmd := m.reload("", "")
	in.back.setCursor(m, name)
	in.back.say(status, false)
	return cmd
}

// setCursor puts the picker's cursor on the Space name.
func (p *spacePicker) setCursor(m *Model, name string) {
	for i, t := range m.st.SpaceSlots() {
		if !t.Free() && t.Name == name {
			p.cursor = i
		}
	}
}
