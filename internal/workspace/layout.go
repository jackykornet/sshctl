package workspace

// minPaneBody is the fewest Session rows the frame's optional lines leave
// the pane.
const minPaneBody = 3

// A frameLayout is where the Workspace's rows go, top to bottom: the Space
// bar and its rule, the main area (the list beside the pane: the pane's
// header, its tab line and the Session), the status rule and the status bar.
type frameLayout struct {
	spaceBar, spaceRule, statusRule, paneHeader bool
	// mainTop and mainH are the first row and the height of the main area.
	mainTop, mainH int
	// tabRow is the screen row of the tab line, and bodyTop the first row
	// of the Session under it.
	tabRow, bodyTop int
}

// frame lays out the Workspace's rows. The tab line and the status bar
// always show; on a short terminal the Space-bar rule goes first, then the
// status rule, then the pane header, so the Session keeps minPaneBody rows.
func (m *Model) frame() frameLayout {
	var f frameLayout
	f.spaceBar = m.st.SeveralSpaces()
	fixed := 2 // the tab line and the status bar
	if f.spaceBar {
		fixed++
	}
	room := m.h - fixed // the Session's rows
	for _, show := range []*bool{&f.paneHeader, &f.statusRule, &f.spaceRule} {
		if show == &f.spaceRule && !f.spaceBar {
			break
		}
		if room-1 < minPaneBody {
			break
		}
		*show = true
		room--
	}
	if f.spaceBar {
		f.mainTop++
	}
	if f.spaceRule {
		f.mainTop++
	}
	f.mainH = max(0, m.statusRow()-f.mainTop)
	if f.statusRule {
		f.mainH = max(0, f.mainH-1)
	}
	f.tabRow = f.mainTop
	if f.paneHeader {
		f.tabRow++
	}
	f.bodyTop = f.tabRow + 1
	return f
}

// statusRow is the screen row of the status bar, the last one.
func (m *Model) statusRow() int { return m.h - 1 }

// mainEnd is the screen row under the main area.
func (f frameLayout) mainEnd() int { return f.mainTop + f.mainH }

// paneSize is the size of the Session's area under the tab line.
func (m *Model) paneSize() (int, int) {
	f := m.frame()
	return max(m.w-m.paneOffset(), 1), max(f.mainEnd()-f.bodyTop, 1)
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
// screen row where they start (under the Space bar, the Space note and the
// filter line, when shown). The Tunnels section, when shown, takes the
// list's last lines.
func (m *Model) listRows() (rows []Row, offset, top int) {
	rows = m.st.Rows()
	f := m.frame()
	top = f.mainTop + len(m.spaceNoteLines(m.listWidth()))
	if m.filtering() || m.st.Filter() != "" {
		top++
	}
	height := f.mainEnd() - top - m.tunnelLayout().height
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
	f := m.frame()
	a := tunnelArea{height: min(1+len(rows), max(2, f.mainH/2)), rows: rows}
	a.top = f.mainEnd() - a.height + 1
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
	if y < a.top || y >= m.frame().mainEnd() || i >= len(a.rows) {
		return 0, false
	}
	return i, true
}
