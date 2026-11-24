package workspace

// paneSize is the size of the terminal area under the tab bar.
func (m *Model) paneSize() (int, int) {
	return max(m.w-m.paneOffset(), 1), max(m.h-2, 1)
}

// syncSize resizes every Session's terminal to the current pane size.
func (m *Model) syncSize() {
	if m.w == 0 {
		return
	}
	m.sessions.resize(m.paneSize())
}

// listWidth is the connection list's width, or 0 when it is hidden.
func (m *Model) listWidth() int {
	if m.hideList || m.w < autoHideWidth {
		return 0
	}
	return min(m.opts.ListWidth, m.w/2)
}

// paneOffset is the column where the session pane starts.
func (m *Model) paneOffset() int {
	if lw := m.listWidth(); lw > 0 {
		return lw + 1
	}
	return 0
}

// listRows returns the visible rows, the scroll offset into them, and the
// screen row where they start (after the Space bar and the filter line,
// when shown). The Tunnels section, when shown, takes the list's last lines.
func (m *Model) listRows() (rows []Row, offset, top int) {
	rows = m.st.Rows()
	if m.st.SeveralSpaces() { // the Space bar
		top++
	}
	top += len(m.spaceNoteLines(m.listWidth()))
	if m.filtering() || m.st.Filter() != "" {
		top++
	}
	height := m.h - 1 - top - m.tunnelLayout().height
	cur := m.st.CursorIndex(rows)
	if height > 0 && cur >= height {
		offset = cur - height + 1
	}
	return rows, offset, top
}

// tunnelArea is where the Tunnels section sits at the bottom of the list.
type tunnelArea struct {
	// height is the section's lines, its title included: a title and a row
	// per Tunnel, at most half the list. It is 0 while no tunnel is listed.
	height int
	// top is the screen row of its first tunnel row, under the title.
	top int
	// first is the first of rows shown, scrolled to keep the cursor's row
	// in view.
	first int
	rows  []tunnelRow
}

// tunnelLayout is the Tunnels section's geometry, which rendering and mouse
// clicks share.
func (m *Model) tunnelLayout() tunnelArea {
	rows := m.tunnelRows()
	if len(rows) == 0 {
		return tunnelArea{}
	}
	a := tunnelArea{height: min(1+len(rows), max(2, (m.h-1)/2)), rows: rows}
	a.top = m.h - 1 - a.height + 1
	if shown := a.height - 1; m.tunnelCursor >= shown {
		a.first = m.tunnelCursor - shown + 1
	}
	return a
}

// tunnelRowAt returns the index of the Tunnels section row at screen
// position x, y.
func (m *Model) tunnelRowAt(x, y int) (int, bool) {
	a := m.tunnelLayout()
	if a.height == 0 || x < 0 || x >= m.listWidth() {
		return 0, false
	}
	i := y - a.top + a.first
	if y < a.top || y >= m.h-1 || i >= len(a.rows) {
		return 0, false
	}
	return i, true
}
