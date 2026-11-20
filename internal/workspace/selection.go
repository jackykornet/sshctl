package workspace

// pos is a cell in a Session pane's body: column x, and row y counted from
// the top of what the pane shows.
type pos struct{ x, y int }

// before reports whether p comes before q in reading order.
func (p pos) before(q pos) bool { return p.y < q.y || p.y == q.y && p.x < q.x }

// selection is text being selected with the mouse in a Session pane: every
// cell from anchor to head in reading order, as the pane showed it at scroll.
type selection struct {
	sess   *Session
	scroll int
	anchor pos
	head   pos
	// dragging is set while the left button is held.
	dragging bool
}

// empty reports whether the selection covers only the cell it started on,
// which selects nothing.
func (s *selection) empty() bool { return s.anchor == s.head }

// shownIn reports whether the selection is drawn over sess's pane, whose
// runner is r: it must be that pane, scrolled as when it was made.
func (s *selection) shownIn(sess *Session, r *runner) bool {
	return s.sess == sess && r != nil && s.scroll == r.scroll && !s.empty()
}

// bounds returns the selection's first and last cell in reading order.
func (s *selection) bounds() (from, to pos) {
	if s.head.before(s.anchor) {
		return s.head, s.anchor
	}
	return s.anchor, s.head
}

// span returns the columns [x0, x1) selected on row y of a pane w columns
// wide, and false when none are.
func (s *selection) span(y, w int) (x0, x1 int, ok bool) {
	from, to := s.bounds()
	if y < from.y || y > to.y {
		return 0, 0, false
	}
	x1 = w
	if y == from.y {
		x0 = from.x
	}
	if y == to.y {
		x1 = min(to.x+1, w)
	}
	return x0, x1, x0 < x1
}
