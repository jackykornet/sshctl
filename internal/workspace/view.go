package workspace

import (
	"fmt"
	"github.com/sshctl/sshctl/internal/catalog"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Colors are basic ANSI colors so they follow the user's terminal theme.
var (
	openStyle   = lipgloss.NewStyle().Foreground(lipgloss.Green)
	endedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Red)
	activeRow   = lipgloss.NewStyle().Reverse(true)
	barStyle    = lipgloss.NewStyle().Reverse(true)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	endedBanner = lipgloss.NewStyle().Reverse(true).Foreground(lipgloss.Yellow)
	unseenStyle = lipgloss.NewStyle().Bold(true)
	dropStyle   = lipgloss.NewStyle().Underline(true).Bold(true)
	// selectionStyle draws text selected with the mouse in a Session pane.
	selectionStyle = lipgloss.NewStyle().Background(lipgloss.Blue).Foreground(lipgloss.BrightWhite)
	// spaceActiveStyle is the accent colour of the active Space in the
	// Space bar.
	spaceActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Cyan).Bold(true)
	// spaceFreeStyle is a free Space number in the Space bar, fainter than
	// an idle Space. The picker draws its free rows in dimStyle.
	spaceFreeStyle = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack).Faint(true)
)

// unseenMarker flags tabs and Connections with activity the user hasn't seen.
const unseenMarker = "•"

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "sshctl"
	if s, r := m.currentRunner(); r != nil && !s.Ended && m.focus == focusPane &&
		m.base().kind == modeList && m.mode.kind != modeModal && m.mode.kind != modeDrag {
		if x, y, visible := r.term.Cursor(); visible {
			v.Cursor = tea.NewCursor(m.paneOffset()+x, 1+y)
		}
	}
	return v
}

// render draws the whole Workspace as m.h lines of exactly m.w columns.
func (m *Model) render() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	// Output may have arrived since the last Update; draw scrolled-back views
	// where they were.
	m.keepViews()
	mainH := m.h - 1
	if h, ok := m.mode.modal.(*helpScreen); ok {
		// Help covers the whole Workspace without resizing the Sessions
		// under it. Its title stays; the bindings scroll below it.
		title, body := m.helpLines()
		lines := make([]string, 0, m.h)
		lines = append(lines, fit(title, m.w))
		top := max(0, min(h.top, m.helpMaxTop()))
		end := min(len(body), top+m.helpRows())
		more := len(body) - end
		if more > 0 {
			end-- // the last row says how much is below
			more++
		}
		for _, l := range body[top:end] {
			lines = append(lines, fit(l, m.w))
		}
		if more > 0 {
			lines = append(lines, fit("  "+dimStyle.Render(fmt.Sprintf("↓ %d more below (↓ / PgDn)", more)), m.w))
		}
		for len(lines) < mainH {
			lines = append(lines, strings.Repeat(" ", m.w))
		}
		return strings.Join(append(lines, m.renderStatus(m.w)), "\n")
	}
	lw := m.listWidth()
	pane := m.renderPane(m.w-m.paneOffset(), mainH)
	var list []string
	if lw > 0 {
		list = m.renderList(lw, mainH)
	}
	lines := make([]string, 0, m.h)
	sep := dimStyle.Render("│")
	for i := range mainH {
		if lw > 0 {
			lines = append(lines, list[i]+sep+pane[i])
		} else {
			lines = append(lines, pane[i])
		}
	}
	lines = append(lines, m.renderStatus(m.w))
	return strings.Join(lines, "\n")
}

func (m *Model) renderList(w, h int) []string {
	section := m.tunnelSection(w)
	h -= len(section)
	rows, offset, top := m.listRows()
	lines := make([]string, 0, h)
	if bar := m.spaceBar(w); bar != "" {
		lines = append(lines, fit(bar, w))
	}
	for _, l := range m.spaceNoteLines(w) {
		lines = append(lines, fit(dimStyle.Render(l), w))
	}
	if len(lines) < top {
		f := "/" + m.st.Filter()
		if m.filtering() {
			f = "/" + m.base().filter.withCursor()
		}
		lines = append(lines, fit(f, w))
	}
	cursor := m.st.CursorIndex(rows)
	active := m.st.Active()
	grouped := m.st.grouped()
	for i, r := range rows[min(offset, len(rows)):] {
		if len(lines) == h {
			break
		}
		mark := pointer(m.focus == focusList && m.tunnelCursor < 0 && offset+i == cursor)
		var line string
		highlight := false
		if r.Header() {
			line = mark + m.headerLabel(r.Group)
			highlight = active != nil && active.Group == r.Group && m.st.Collapsed(r.Group) && m.st.Filter() == ""
		} else {
			space := ""
			if m.st.CrossSpace() {
				space = m.st.SpaceName(r.Conn.Space)
			}
			line = mark + connLabel(r.Conn, grouped && space == "", space, m.tunnels.running(r.Conn.Alias))
			highlight = r.Conn == active
		}
		line = fit(line, w)
		switch d := m.mode.drag; {
		case d != nil && d.moving && d.group == "" && !r.Header() && r.Conn.Alias == d.alias,
			d != nil && d.moving && d.group != "" && r.Header() && r.Group == d.group:
			line = dimStyle.Render(ansi.Strip(line))
		case d != nil && d.moving && d.ok && r.Group == d.target:
			line = dropStyle.Render(ansi.Strip(line))
		case highlight:
			line = activeRow.Render(ansi.Strip(line))
		}
		lines = append(lines, line)
	}
	if len(rows) == 0 && len(lines) < h {
		msg := "no hosts in ssh_config"
		if m.st.Filter() != "" {
			msg = "no matches"
		}
		lines = append(lines, fit(dimStyle.Render(" "+msg), w))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return append(lines, section...)
}

// tunnelMarker flags a Connection with a running tunnel, and starts each
// row of the Tunnels section.
const tunnelMarker = "⇄"

// tunnelSection is the Tunnels section's lines, w columns wide: a title,
// then one row per Tunnel of every tunnel, from every Space, scrolled to
// keep the cursor's row in view.
func (m *Model) tunnelSection(w int) []string {
	s := m.tunnelLayout()
	if s.height == 0 {
		return nil
	}
	title := "─ Tunnels "
	lines := []string{dimStyle.Render(fit(title+strings.Repeat("─", max(0, w-len([]rune(title)))), w))}
	for i, r := range s.rows[s.first:min(len(s.rows), s.first+s.height-1)] {
		lines = append(lines, m.tunnelLine(r, s.first+i == m.tunnelCursor && m.focus == focusList, w))
	}
	return lines
}

// pointer starts a list or Tunnels section row: › on the cursor's row.
func pointer(cursor bool) string {
	if cursor {
		return "›"
	}
	return " "
}

// tunnelLine is one row of the Tunnels section, such as
// ⇄ prod-web panel :8083 ●, or ⇄ 2·vps panel :8084 ● for a Connection in
// another Space. A failed tunnel ends in ✕ and ssh's error.
func (m *Model) tunnelLine(r tunnelRow, cursor bool, w int) string {
	name := r.run.alias
	if n := m.st.OtherSpaceNumber(r.run.alias, r.run.space); n > 0 {
		name = dimStyle.Render(fmt.Sprintf("%d·", n)) + name
	}
	if r.tunnel.Name != "" {
		name += " " + r.tunnel.Name
	}
	state, detail := openStyle.Render("●"), ""
	if r.run.failed() {
		state, detail = errStyle.Render("✕"), " "+dimStyle.Render(r.run.err)
	}
	right := fmt.Sprintf(" :%d %s", r.tunnel.LocalPort, state)
	left := pointer(cursor) + tunnelMarker + " "
	room := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if ansi.StringWidth(name) > room {
		name = ansi.Truncate(name, max(room, 1), "…")
	}
	return fit(left+name+right+detail, w)
}

// spaceBar shows every Space number, [1] [2] [3] … [9]: the active Space
// in the accent colour, one with a live Session in normal text, an idle one
// dimmed, and a free number fainter still. When that is wider than w, the
// numbers close up, [1][2][3]. It is empty while there is only one Space.
func (m *Model) spaceBar(w int) string {
	if !m.st.SeveralSpaces() {
		return ""
	}
	var parts []string
	for _, t := range m.st.SpaceSlots() {
		if t.Number == 0 {
			continue // beyond the ninth: only the picker lists it
		}
		label := fmt.Sprintf("[%d]", t.Number)
		switch {
		case t.Free():
			label = spaceFreeStyle.Render(label)
		case t.Active:
			label = spaceActiveStyle.Render(label)
		case !t.Live:
			label = dimStyle.Render(label)
		}
		parts = append(parts, label)
	}
	if bar := " " + strings.Join(parts, " "); ansi.StringWidth(bar) <= w {
		return bar
	}
	return " " + strings.Join(parts, "")
}

// spaceNames is every Space as "number name" in number order, a Space
// without a number by its name alone.
func (m *Model) spaceNames() []string {
	tabs := m.st.SpaceTabs()
	names := make([]string, len(tabs))
	for i, t := range tabs {
		names[i] = t.Name
		if t.Number > 0 {
			names[i] = fmt.Sprintf("%d %s", t.Number, t.Name)
		}
	}
	return names
}

// spaceNoteLines is the Space note, 1 homelab · 2 customer1 · 3 prive,
// wrapped between Spaces to w columns; nil while it is hidden.
func (m *Model) spaceNoteLines(w int) []string {
	if !m.spaceNote || w <= 0 {
		return nil
	}
	var lines []string
	line := ""
	names := m.spaceNames()
	for i, name := range names {
		next := line + " · " + name
		if i < len(names)-1 {
			next += " ·" // room for the separator should the line wrap after it
		}
		switch {
		case line == "":
			line = " " + name
		case ansi.StringWidth(next) <= w:
			line += " · " + name
		default:
			lines = append(lines, line+" ·")
			line = " " + name
		}
	}
	return append(lines, line)
}

// headerLabel is a group header: its fold marker, Group number (for a stored
// Group), name and member count, then how many members are open and whether
// any has unseen activity.
func (m *Model) headerLabel(group string) string {
	fold := "▾ "
	if m.st.Collapsed(group) && m.st.Filter() == "" {
		fold = "▸ "
	}
	name := boldStyle.Render(group)
	if n := m.st.GroupPosition(group); n > 0 {
		name = dimStyle.Render(fmt.Sprint(n)) + " " + name
	}
	if group == "" {
		name = dimStyle.Render("ungrouped")
	}
	members := m.st.Members(group)
	open, unseen := 0, false
	for _, c := range members {
		if c.Open() {
			open++
		}
		unseen = unseen || c.Unseen()
	}
	count := fmt.Sprint(len(members))
	if len(members) == 0 && group != "" {
		count = "(empty)"
	}
	line := fold + name + " " + dimStyle.Render(count)
	if open > 0 {
		line += " " + openStyle.Render(fmt.Sprintf("●%d", open))
	}
	if unseen {
		line += " " + unseenStyle.Render(unseenMarker)
	}
	return line
}

// connLabel is a Connection row after the cursor pointer, indented under its
// group header when the list is grouped. A space names the Connection's
// Space, for a filter across every Space. tunnel adds ⇄ for a running
// tunnel, apart from the ● of an open Connection.
func connLabel(c *Connection, grouped bool, space string, tunnel bool) string {
	marker, name := "  ", c.Alias
	switch {
	case c.AllEnded():
		marker, name = endedStyle.Render("● "), endedStyle.Render(c.Alias)
	case c.Open():
		marker, name = openStyle.Render("● "), openStyle.Render(c.Alias)
	case c.Orphaned:
		name = dimStyle.Render(c.Alias)
	}
	line := marker + name
	if grouped {
		line = "  " + line
	}
	if tunnel {
		line += " " + openStyle.Render(tunnelMarker)
	}
	if c.Unseen() {
		line += " " + unseenStyle.Render(unseenMarker)
	}
	if extra := rowDetail(c, grouped, space); extra != "" {
		line += " " + dimStyle.Render(extra)
	}
	return line
}

// rowDetail is a Connection's description, or (missing) when it is orphaned.
// The group is added only when the list has no group headers to show it,
// after the Connection's space when that is given: [space/group].
func rowDetail(c *Connection, grouped bool, space string) string {
	if c.Orphaned {
		if space != "" {
			return "(missing) [" + space + "]"
		}
		return "(missing)"
	}
	d := c.Description
	where := c.Group
	if grouped {
		where = ""
	}
	if space != "" {
		where = strings.TrimSuffix(space+"/"+where, "/")
	}
	if where != "" {
		d = strings.TrimSpace(d + " [" + where + "]")
	}
	return d
}

type tabSpan struct{ start, end, index int }

// tabBar renders the Active connection's tabs in width columns, scrolling so
// the current tab stays visible, and reports where each visible tab sits.
func (m *Model) tabBar(width int) (string, []tabSpan) {
	c := m.st.Active()
	if c == nil || !c.Open() || width <= 0 {
		return strings.Repeat(" ", max(width, 0)), nil
	}
	labels := make([]string, len(c.Sessions))
	styles := make([]lipgloss.Style, len(c.Sessions))
	for i, s := range c.Sessions {
		label := fmt.Sprintf("%s %d", c.Alias, s.N)
		switch {
		case s.Action != "":
			label += " " + string(s.Action)
		case s.Kind == LaunchClaude:
			label += " claude"
		}
		marker, style := tabState(s)
		labels[i] = " " + label + marker + " "
		styles[i] = style
		if i == c.Current {
			styles[i] = activeRow
		}
	}
	// Grow a window of tabs around the current one while it fits, leaving room for ‹ and ›.
	avail := width - 2
	first, last := c.Current, c.Current
	used := ansi.StringWidth(labels[c.Current])
	for {
		grew := false
		if last+1 < len(labels) && used+ansi.StringWidth(labels[last+1]) <= avail {
			last++
			used += ansi.StringWidth(labels[last])
			grew = true
		}
		if first > 0 && used+ansi.StringWidth(labels[first-1]) <= avail {
			first--
			used += ansi.StringWidth(labels[first])
			grew = true
		}
		if !grew {
			break
		}
	}
	var b strings.Builder
	var spans []tabSpan
	x := 0
	if first > 0 {
		b.WriteString(dimStyle.Render("‹"))
		x++
	}
	for i := first; i <= last; i++ {
		lw := ansi.StringWidth(labels[i])
		spans = append(spans, tabSpan{x, x + lw, i})
		b.WriteString(styles[i].Render(labels[i]))
		x += lw
	}
	if last < len(labels)-1 {
		b.WriteString(dimStyle.Render("›"))
	}
	return fit(b.String(), width), spans
}

// tabState returns a tab's state marker (with its leading space, or empty)
// and the style for a tab that is not current. Ended wins over activity.
func tabState(s *Session) (marker string, style lipgloss.Style) {
	switch {
	case s.Ended:
		return " ✕", endedStyle
	case s.Unseen:
		return " " + unseenMarker, unseenStyle
	default:
		return "", dimStyle
	}
}

func (m *Model) renderPane(w, h int) []string {
	if f := m.form(); f != nil {
		return f.render(w, h, m.spaceNames(), f.groups(m.st), m.features.On(catalog.FeatureBootstrap))
	}
	if p := m.picker(); p != nil {
		return p.render(m, w, h)
	}
	bar, _ := m.tabBar(w)
	lines := []string{bar}
	var body []string
	s, r := m.currentRunner()
	switch {
	case s == nil:
		body = []string{"", "  " + dimStyle.Render("Select a connection and press Enter.")}
	case r != nil:
		body = r.term.Lines(r.scroll)
	}
	sel := m.mode.sel
	if sel != nil && !sel.shownIn(s, r) {
		sel = nil
	}
	for y, l := range body {
		if len(lines) == h {
			break
		}
		l = fit(l, w)
		if sel != nil {
			if x0, x1, ok := sel.span(y, w); ok {
				l = highlight(l, x0, x1, w)
			}
		}
		// Reset after each line so remote styles never bleed into the separator.
		lines = append(lines, l+"\x1b[m")
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	if s != nil && s.Ended && h > 1 {
		banner := fmt.Sprintf(" session ended (exit %d) · %s ", s.ExitCode, m.endedHints())
		lines[h-1] = endedBanner.Render(fit(banner, w))
	}
	return lines
}

// highlight draws columns [x0, x1) of line, which is w columns wide, in
// selectionStyle, dropping the Session's own styles there.
func highlight(line string, x0, x1, w int) string {
	return ansi.Cut(line, 0, x0) + "\x1b[m" + selectionStyle.Render(ansi.Strip(ansi.Cut(line, x0, x1))) + ansi.Cut(line, x1, w)
}

// helpRows is how many rows the help screen has for bindings, below its
// title and above the status line.
func (m *Model) helpRows() int { return max(1, m.h-2) }

// helpMaxTop is the furthest the help screen scrolls: the top line that
// shows the last binding on the last row.
func (m *Model) helpMaxTop() int { return max(0, len(bindings)-m.helpRows()) }

// helpLines returns the help screen's title and a line for every binding
// (see bindings) with its keys and help text.
func (m *Model) helpLines() (title string, body []string) {
	labels := make([]string, len(bindings))
	width := 0
	for i, b := range bindings {
		labels[i] = m.bindingLabel(b)
		width = max(width, ansi.StringWidth(labels[i]))
	}
	title = "  " + boldStyle.Render("Keys") + dimStyle.Render("  "+(&helpScreen{}).hints()[0].String())
	for i, b := range bindings {
		pad := strings.Repeat(" ", width-ansi.StringWidth(labels[i]))
		body = append(body, "  "+labels[i]+pad+"  "+dimStyle.Render(m.helpText(b)))
	}
	return title, body
}

func (m *Model) renderStatus(w int) string {
	var left string
	modalStatus, ok := "", false
	if m.mode.modal != nil {
		modalStatus, ok = m.mode.modal.status()
	}
	d := m.mode.drag
	switch {
	case ok:
		left = modalStatus
	case m.spaceNote && m.listWidth() == 0:
		left = strings.Join(m.spaceNames(), " · ")
	case d != nil && d.moving && d.group != "":
		left = "drop group " + d.group + " on another group header · release off the list to cancel"
		if d.ok {
			left = fmt.Sprintf("move group %s to position %d · release to drop", d.group, m.st.GroupPosition(d.target))
		}
	case d != nil && d.moving:
		left = "drop " + d.alias + " on a group · release off the list to cancel"
		if d.ok {
			to := d.target
			if to == "" {
				to = "ungrouped"
			}
			left = fmt.Sprintf("move %s → %s · release to drop", d.alias, to)
		}
	case m.status != "" && m.statusErr:
		left = errStyle.Render(m.status)
	case m.status != "":
		left = m.status
	case m.scrolling():
		left = "scroll mode · " + m.scrollHints().String()
		if _, r := m.currentRunner(); r != nil {
			left = fmt.Sprintf("scroll: %d/%d lines back · %s", r.scroll, r.term.ScrollbackLen(), m.scrollHints())
		}
	default:
		left = m.sessionStatus()
	}
	if m.prefix || m.mode.modal != nil && m.mode.modal.prefixArmed() {
		left = keyHint(m.opts.PrefixKey) + " … " + left
	}
	line := " " + left
	for _, hints := range m.statusHints() {
		right := hints.String()
		if gap := w - ansi.StringWidth(line) - ansi.StringWidth(right) - 1; gap >= 1 {
			line += strings.Repeat(" ", gap) + right + " "
			break
		}
	}
	return barStyle.Render(fit(line, w))
}

// statusHints are the status line's hint lines for the right side, the
// longest first; it shows the first that fits.
func (m *Model) statusHints() []hintLine {
	return []hintLine{m.hint(inPrefix, "?", "n", "w"), m.hint(inPrefix, "?")}
}

// endedHints is the ended tab banner's hint line.
func (m *Model) endedHints() hintLine { return m.hint(inEnded, "r", "x") }

func (m *Model) sessionStatus() string {
	key := "OpenSSH default"
	if k := m.identities.SpaceIdentity(m.st.Space()); k != "" {
		key = filepath.Base(k)
	}
	space := ""
	if n := m.tunnels.count(); n > 0 {
		space = tunnelMarker + " " + plural(n, "tunnel") + " · "
	}
	if m.st.SeveralSpaces() {
		space += m.st.ActiveSpaceName() + " · "
	}
	c := m.st.Active()
	if c == nil {
		return space + "no active connection · key: " + key
	}
	s := c.CurrentSession()
	state := "connected"
	if s.Ended {
		state = fmt.Sprintf("ended (exit %d)", s.ExitCode)
	}
	return fmt.Sprintf("%s%s · tab %d/%d · %s · key: %s", space, c.Alias, c.Current+1, len(c.Sessions), state, key)
}

// fit truncates or pads s to exactly w columns.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}
