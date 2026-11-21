package workspace

// modeKind is what the Workspace is doing with its input. List, filter and
// scroll are base modes; a drag, a selection and a modal are entered over
// a base mode and return to it.
type modeKind int

const (
	// modeList: keys go to the list or the focused Session.
	modeList modeKind = iota
	// modeFilter: keys type the filter.
	modeFilter
	// modeScroll: keys scroll the focused pane's history.
	modeScroll
	// modeDrag: a Connection or Group header is held with the mouse.
	modeDrag
	// modeSelect: text is (being) selected with the mouse in a pane.
	modeSelect
	// modeModal: a modal gets every key and paste; see modal.
	modeModal
)

// over reports whether k is entered over a base mode.
func (k modeKind) over() bool { return k >= modeDrag }

// mode is the Workspace's mode and the data it needs. Only the field of its
// kind is set. The prefix is not a mode: it can be armed in any of them.
type mode struct {
	kind modeKind
	// filter is the filter being typed, with its cursor (modeFilter);
	// State holds its text.
	filter line
	drag   *drag      // modeDrag
	sel    *selection // modeSelect
	modal  modal      // modeModal
	// back is the base mode a drag, selection or modal returns to.
	back *mode
}

// setMode is the one place the Workspace's mode changes. Leaving a mode
// drops what belonged to it: a drag stops showing the ungrouped header.
func (m *Model) setMode(next mode) {
	if next.kind.over() {
		if next.back == nil {
			b := *m.base()
			next.back = &b
		}
	} else {
		next.back = nil
	}
	if d := m.mode.drag; d != nil && next.drag != d {
		m.st.ShowUngrouped(false)
	}
	m.mode = next
}

// base returns the base mode: the current one, or the one a drag,
// selection or modal returns to.
func (m *Model) base() *mode {
	if m.mode.back != nil {
		return m.mode.back
	}
	return &m.mode
}

// setBase changes the base mode, under any drag, selection or modal.
func (m *Model) setBase(b mode) {
	if !m.mode.kind.over() {
		m.setMode(b)
		return
	}
	cur := m.mode
	cur.back = &b
	m.setMode(cur)
}

// leave ends a drag, selection or modal, returning to the base mode.
func (m *Model) leave() { m.setMode(*m.base()) }

func (m *Model) filtering() bool { return m.base().kind == modeFilter }
func (m *Model) scrolling() bool { return m.base().kind == modeScroll }

// stopScroll leaves scroll mode, if the Workspace is in it.
func (m *Model) stopScroll() {
	if m.scrolling() {
		m.setBase(mode{kind: modeList})
	}
}

// clearSelection drops the selection, if there is one.
func (m *Model) clearSelection() {
	if m.mode.kind == modeSelect {
		m.leave()
	}
}

// showModal makes md the modal, keeping the base mode an open modal returns
// to; nil closes the modal.
func (m *Model) showModal(md modal) {
	if md != nil {
		m.setMode(mode{kind: modeModal, modal: md})
	} else if m.mode.kind == modeModal {
		m.leave()
	}
}
