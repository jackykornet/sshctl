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
// when shown).
func (m *Model) listRows() (rows []Row, offset, top int) {
	rows = m.st.Rows()
	if m.st.SeveralSpaces() { // the Space bar
		top++
	}
	top += len(m.spaceNoteLines(m.listWidth()))
	if m.filtering() || m.st.Filter() != "" {
		top++
	}
	height := m.h - 1 - top
	cur := m.st.CursorIndex(rows)
	if height > 0 && cur >= height {
		offset = cur - height + 1
	}
	return rows, offset, top
}
