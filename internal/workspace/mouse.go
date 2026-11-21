package workspace

import (
	"fmt"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// doubleClickTime is the longest time between the two clicks of a double-click.
var doubleClickTime = 500 * time.Millisecond

// paneClick is a left click in a Session's pane, kept to recognize the
// second click of a double-click.
type paneClick struct {
	sess *Session
	at   pos
	when time.Time
}

// drag is a Connection, or a Group header when group is set, held down with
// the left mouse button in the list.
type drag struct {
	alias string
	group string
	fromY int
	// moving is set once the pointer leaves the row it was pressed on.
	moving bool
	// target is the group under the pointer; ok is false off the list, and
	// for a header drag anywhere but another stored Group's header.
	target string
	ok     bool
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.modal != nil {
		return nil
	}
	mm := msg.Mouse()
	if m.sel != nil && m.sel.dragging {
		switch msg.(type) {
		case tea.MouseMotionMsg:
			m.sel.head = m.panePos(mm)
			return nil
		case tea.MouseReleaseMsg:
			return m.copySelection(mm)
		}
	}
	if _, ok := msg.(tea.MouseClickMsg); ok {
		m.sel = nil
	}
	if m.drag != nil {
		switch msg.(type) {
		case tea.MouseMotionMsg:
			m.dragOver(mm)
			return nil
		case tea.MouseReleaseMsg:
			return m.drop(mm)
		}
	}
	if mm.Y >= m.h-1 {
		return nil
	}
	lw := m.listWidth()
	if mm.X < lw {
		return m.listMouse(msg, mm)
	}
	paneX := mm.X - m.paneOffset()
	if paneX < 0 {
		return nil // the separator column
	}
	if mm.Y == 0 {
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			_, spans := m.tabBar(m.w - m.paneOffset())
			for _, sp := range spans {
				if paneX >= sp.start && paneX < sp.end {
					m.st.SelectTabIndex(sp.index)
					m.scrolling = false
				}
			}
			if m.st.Active() != nil {
				m.focus = focusPane
			}
		}
		return nil
	}
	s, r := m.currentRunner()
	if s == nil || r == nil {
		return nil
	}
	if _, ok := msg.(tea.MouseClickMsg); ok {
		m.focus = focusPane
	}
	// Full-screen programs get the mouse; otherwise the wheel scrolls history.
	if r.term.AltScreen() && !s.Ended {
		r.term.SendMouse(msg, paneX, mm.Y-1)
		return nil
	}
	switch msg.(type) {
	case tea.MouseWheelMsg:
		m.sel = nil
		switch mm.Button {
		case tea.MouseWheelUp:
			m.scrollBy(r, wheelLines)
		case tea.MouseWheelDown:
			m.scrollBy(r, -wheelLines)
		}
	case tea.MouseClickMsg:
		switch {
		case mm.Button == tea.MouseLeft:
			// Output shown before the press must not clear the new selection.
			m.sessions.takeActivity()
			p := m.panePos(mm)
			if m.doubleClick(s, p) {
				return m.selectWord(s, r, p)
			}
			m.sel = &selection{sess: s, scroll: r.scroll, anchor: p, head: p, dragging: true}
		case mm.Button == tea.MouseRight && !s.Ended:
			return m.startPaste(s)
		}
	}
	return nil
}

// panePos returns the pane body cell under the pointer, clamped to the pane.
func (m *Model) panePos(mm tea.Mouse) pos {
	w, h := m.paneSize()
	return pos{
		x: max(0, min(mm.X-m.paneOffset(), w-1)),
		y: max(0, min(mm.Y-1, h-1)),
	}
}

// copySelection ends a selection drag at the pointer and copies the selected
// text to the system clipboard. A press and release on one cell selects nothing.
func (m *Model) copySelection(mm tea.Mouse) tea.Cmd {
	m.sel.head, m.sel.dragging = m.panePos(mm), false
	return m.copySelected()
}

// doubleClick records a left click at p in sess's pane and reports whether
// it is the second of a double-click: on the same cell as the click before,
// within doubleClickTime.
func (m *Model) doubleClick(sess *Session, p pos) bool {
	now := time.Now()
	prev := m.lastClick
	m.lastClick = paneClick{sess: sess, at: p, when: now}
	if prev.sess == sess && prev.at == p && now.Sub(prev.when) < doubleClickTime {
		m.lastClick = paneClick{} // a third click starts over
		return true
	}
	return false
}

// selectWord selects the word at p in sess's pane, whose runner is r, and
// copies it. Off a word it selects nothing.
func (m *Model) selectWord(sess *Session, r *runner, p pos) tea.Cmd {
	from, to, ok := r.term.Word(r.scroll, p)
	if !ok {
		m.sel = nil
		return nil
	}
	m.sel = &selection{sess: sess, scroll: r.scroll, anchor: from, head: to, word: true}
	return m.copySelected()
}

// copySelected copies the selected text to the system clipboard and says how
// much it copied. A selection with no text is dropped.
func (m *Model) copySelected() tea.Cmd {
	sel := m.sel
	r := m.sessions.runner(sel.sess)
	if sel.empty() || r == nil {
		m.sel = nil
		return nil
	}
	from, to := sel.bounds()
	text := r.term.Text(sel.scroll, from, to)
	if text == "" {
		m.sel = nil
		return nil
	}
	return tea.Batch(tea.SetClipboard(text), m.setStatus(fmt.Sprintf("copied %d characters", utf8.RuneCountInString(text)), false))
}

func (m *Model) listMouse(msg tea.MouseMsg, mm tea.Mouse) tea.Cmd {
	switch msg.(type) {
	case tea.MouseWheelMsg:
		switch mm.Button {
		case tea.MouseWheelUp:
			m.st.MoveCursor(-1)
		case tea.MouseWheelDown:
			m.st.MoveCursor(1)
		}
	case tea.MouseClickMsg:
		if mm.Button != tea.MouseLeft {
			return nil
		}
		row, ok := m.rowAt(mm.X, mm.Y)
		if !ok {
			return nil
		}
		if row.Header() {
			// Released where it was pressed, it toggles; see drop.
			m.st.SetCursorGroup(row.Group)
			m.focus = focusList
			m.drag = &drag{group: row.Group, fromY: mm.Y}
			return nil
		}
		c := row.Conn
		if !m.st.CrossSpace() {
			// A match from another Space has another Space's Groups to drop on.
			m.drag = &drag{alias: c.Alias, fromY: mm.Y}
		}
		saved := m.showSpaceOf(c)
		if c.Open() {
			m.st.Activate(c.Alias)
			m.focus, m.scrolling = focusPane, false
		} else {
			m.st.SetCursor(c.Alias)
			m.focus = focusList
		}
		return saved
	}
	return nil
}

// rowAt returns the list row at screen position x, y.
func (m *Model) rowAt(x, y int) (Row, bool) {
	if x < 0 || x >= m.listWidth() || y >= m.h-1 {
		return Row{}, false
	}
	rows, offset, top := m.listRows()
	i := y - top + offset
	if y < top || i < 0 || i >= len(rows) {
		return Row{}, false
	}
	return rows[i], true
}

// dragOver follows a held Connection. Once it leaves its own row the list
// also shows the ungrouped header, so it can be dropped out of its group.
func (m *Model) dragOver(mm tea.Mouse) {
	d := m.drag
	if !d.moving {
		if mm.Y == d.fromY || mm.Button != tea.MouseLeft {
			return
		}
		d.moving = true
		if d.group == "" {
			m.st.ShowUngrouped(true)
		}
	}
	row, ok := m.rowAt(mm.X, mm.Y)
	if d.group != "" {
		ok = ok && row.Header() && row.Group != d.group && m.st.GroupPosition(row.Group) > 0
	}
	d.target, d.ok = row.Group, ok
}

// drop moves the held Connection to the group of the row it is released on,
// or the held Group to the position of the Group header it is released on.
// Released off the list, or without having moved, nothing changes, except
// that a Group header pressed and released in place toggles.
func (m *Model) drop(mm tea.Mouse) tea.Cmd {
	d := m.drag
	m.drag = nil
	if d.group != "" {
		switch {
		case !d.moving:
			return m.setCollapsed(d.group, !m.st.Collapsed(d.group))
		case d.ok:
			return m.moveGroup(d.group, m.st.GroupPosition(d.target))
		}
		return m.setStatus("move cancelled", false)
	}
	if !d.moving {
		return nil
	}
	row, ok := m.rowAt(mm.X, mm.Y)
	m.st.ShowUngrouped(false)
	if !ok {
		return m.setStatus("move cancelled", false)
	}
	return m.moveTo(d.alias, row.Group)
}
