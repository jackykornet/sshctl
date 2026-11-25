package workspace

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// historyPicker lists History in the pane, most recent first, from every
// Space, filtered by what is typed. Enter opens the Connection under the
// cursor, or switches to it when it is open, making its Space the active
// Space. An Orphaned connection, or an alias with no Connection left, is
// listed as (missing) and does not open.
type historyPicker struct {
	prefixGate
	aliases []string // History, most recent first
	filter  line
	cursor  int // index into rows
	// note is why the last Enter opened nothing, shown on the status line
	// until the next key.
	note string
}

// historyRow is one History entry as the picker lists it. conn is nil when
// no Connection has the alias any more.
type historyRow struct {
	alias string
	conn  *Connection
	space string // the Space's name, "" when there is no Connection
}

// missing reports whether the row's Connection cannot be opened.
func (r historyRow) missing() bool { return r.conn == nil || r.conn.Orphaned }

// openHistory opens the History picker, reading History fresh.
func (m *Model) openHistory() tea.Cmd {
	aliases, err := m.opts.Catalog.History()
	if err != nil {
		return m.setStatus("history: "+err.Error(), true)
	}
	m.openModal(&historyPicker{aliases: aliases})
	return nil
}

// history returns the History picker when it is the modal.
func (m *Model) history() *historyPicker {
	p, _ := m.mode.modal.(*historyPicker)
	return p
}

// rows is History as the picker lists it: the entries whose alias,
// description, Group or Space contains the filter, ignoring case.
func (p *historyPicker) rows(m *Model) []historyRow {
	f := strings.ToLower(p.filter.String())
	var out []historyRow
	for _, alias := range p.aliases {
		r := historyRow{alias: alias, conn: m.st.Get(alias)}
		fields := []string{alias}
		if r.conn != nil {
			r.space = m.st.SpaceName(r.conn.Space)
			fields = append(fields, r.conn.Description, r.conn.Group, r.space)
		}
		if containsFold(fields, f) {
			out = append(out, r)
		}
	}
	return out
}

// containsFold reports whether any of fields contains f, which is
// already lower case.
func containsFold(fields []string, f string) bool {
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), f) {
			return true
		}
	}
	return false
}

func (p *historyPicker) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := p.intercept(m, msg); ok {
		return cmd
	}
	p.note = ""
	if bk, ok := findKey(p.keys(), msg.String()); ok {
		return bk.run(m, msg)
	}
	if p.filter.key(msg) {
		p.cursor = 0
	}
	return nil
}

func (p *historyPicker) keys() []boundKey {
	move := func(delta int) action {
		return movePickerCursor(&p.cursor, delta, func(m *Model) int { return len(p.rows(m)) })
	}
	enter := func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		rows := p.rows(m)
		if p.cursor < 0 || p.cursor >= len(rows) {
			return nil
		}
		r := rows[p.cursor]
		if r.missing() {
			p.note = r.alias + " is missing from ssh_config"
			return nil
		}
		m.showModal(nil)
		return m.openOrSwitch(r.conn)
	}
	return []boundKey{
		on("enter", enter).as("open"),
		on("up", move(-1)).as("move"), on("down", move(1)).as("move"),
		on("esc", closeModal).as("close"), on("ctrl+c", closeModal),
	}
}

func (p *historyPicker) hints() []hintLine {
	if p.swallowed {
		return []hintLine{hintsFor(p.keys(), "esc")}
	}
	return []hintLine{hintsFor(p.keys(), "enter", "up", "down", "esc")}
}

func (p *historyPicker) paste(text string) {
	p.filter.paste(text)
	p.cursor = 0
}

func (p *historyPicker) status() (string, bool) {
	switch {
	case p.swallowed:
		return "history open  " + p.hints()[0].String(), true
	case p.note != "":
		return errStyle.Render(p.note), true
	}
	return "history: " + p.filter.withCursor() + "  " + p.hints()[0].String(), true
}

// render draws the picker in the w by h pane: a row per History entry with
// its alias, Space and description, a missing one dimmed as (missing).
func (p *historyPicker) renderPane(m *Model, w, h int) []string {
	rows := p.rows(m)
	aliasW, spaceW := 0, 0
	for _, r := range rows {
		aliasW = max(aliasW, ansi.StringWidth(r.alias))
		spaceW = max(spaceW, ansi.StringWidth(r.space))
	}
	lines := []string{fit(" "+boldStyle.Render("History"), w)}
	if len(rows) == 0 {
		lines = append(lines, fit("   "+dimStyle.Render("nothing in History matches"), w))
	}
	// Keep the cursor's row in view below the title.
	first := max(0, p.cursor-(h-2))
	for i := first; i < len(rows); i++ {
		r := rows[i]
		pointer := "  "
		if i == p.cursor {
			pointer = " ›"
		}
		alias := r.alias + strings.Repeat(" ", aliasW-ansi.StringWidth(r.alias))
		space := r.space + strings.Repeat(" ", spaceW-ansi.StringWidth(r.space))
		var row string
		if r.missing() {
			row = dimStyle.Render(alias + "  " + space + "  (missing)")
		} else {
			row = alias + "  " + dimStyle.Render(space) + "  " + dimStyle.Render(r.conn.Description)
		}
		lines = append(lines, fit(pointer+" "+row, w))
	}
	return padLines(lines, w, h)
}
