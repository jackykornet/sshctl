package workspace

import (
	"fmt"
	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/openssh"
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
			v.Cursor = tea.NewCursor(m.paneOffset()+x, m.frame().bodyTop+y)
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
	f := m.frame()
	lw := m.listWidth()
	lines := make([]string, 0, m.h)
	if f.spaceBar {
		lines = append(lines, fit(m.spaceBar(m.w), m.w))
	}
	if f.spaceRule {
		lines = append(lines, m.rule("┬"))
	}
	pane := m.renderPane(m.w-m.paneOffset(), f)
	var list []string
	if lw > 0 {
		list = m.renderList(lw, f.mainH)
	}
	sep := dimStyle.Render("│")
	for i := range f.mainH {
		if lw > 0 {
			lines = append(lines, list[i]+sep+pane[i])
		} else {
			lines = append(lines, pane[i])
		}
	}
	if f.statusRule {
		lines = append(lines, m.rule("┴"))
	}
	lines = append(lines, m.renderStatus(m.w))
	return strings.Join(lines, "\n")
}

// rule is a full-width dim line, with join where it meets the line between
// the list and the pane.
func (m *Model) rule(join string) string {
	r := strings.Repeat("─", m.w)
	if lw := m.listWidth(); lw > 0 {
		r = strings.Repeat("─", lw) + join + strings.Repeat("─", m.w-lw-1)
	}
	return dimStyle.Render(r)
}

func (m *Model) renderList(w, h int) []string {
	section := m.tunnelSection(w)
	h -= len(section)
	rows, offset, top := m.listRows()
	top -= m.frame().mainTop // rows above the list's own
	lines := make([]string, 0, h)
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
			line = m.headerLabel(r.Group)
			highlight = active != nil && active.Group == r.Group && m.st.Collapsed(r.Group) && m.st.Filter() == ""
		} else {
			cross := m.st.CrossSpace()
			path := ""
			if cross && r.Conn.Space != m.st.Space() {
				path = m.st.SpaceName(r.Conn.Space)
				if r.Conn.Group != "" {
					path += pathSep + r.Conn.Group
				}
			}
			line = connLabel(r.Conn, grouped && !cross, path, m.tunnels.running(r.Conn.Alias), w-1)
			highlight = r.Conn == active
		}
		line = fit(line, w-1)
		switch d := m.mode.drag; {
		case d != nil && d.moving && d.group == "" && !r.Header() && r.Conn.Alias == d.alias,
			d != nil && d.moving && d.group != "" && r.Header() && r.Group == d.group:
			line = dimStyle.Render(ansi.Strip(line))
		case d != nil && d.moving && d.ok && r.Group == d.target:
			line = dropStyle.Render(ansi.Strip(line))
		case highlight:
			// The bar runs to the list's edge; the cursor's › stays apart.
			line = activeRow.Render(ansi.Strip(line))
		}
		lines = append(lines, fit(mark, 1)+line)
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

// spaceBar is the Workspace's top row: every Space as [1] homelab, the
// active Space in the accent colour, one with a live Session in normal
// text and an idle one dimmed. When the names do not fit in w columns it
// shows every Space number instead, [1] [2] [3] … [9], a free number
// fainter still, and closes them up, [1][2][3], when even that is wider.
// It is empty while there is only one Space.
func (m *Model) spaceBar(w int) string {
	if !m.st.SeveralSpaces() {
		return ""
	}
	var named, numbers []string
	for _, t := range m.st.SpaceSlots() {
		if t.Number == 0 {
			continue // beyond the ninth: only the picker lists it
		}
		style := lipgloss.NewStyle()
		switch {
		case t.Free():
			style = spaceFreeStyle
		case t.Active:
			style = spaceActiveStyle
		case !t.Live:
			style = dimStyle
		}
		label := fmt.Sprintf("[%d]", t.Number)
		numbers = append(numbers, style.Render(label))
		if !t.Free() {
			named = append(named, style.Render(label+" "+t.Name))
		}
	}
	for _, bar := range []string{" " + strings.Join(named, "  "), " " + strings.Join(numbers, " ")} {
		if ansi.StringWidth(bar) <= w {
			return bar
		}
	}
	return " " + strings.Join(numbers, "")
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

// headerLabel is a group header, ▾ 1 web ●1 •: its fold marker, Group
// number (for a stored Group), name, its member count while collapsed, then
// how many members are open and whether any has unseen activity.
func (m *Model) headerLabel(group string) string {
	collapsed := m.st.Collapsed(group) && m.st.Filter() == ""
	fold := "▾ "
	if collapsed {
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
	line := fold + name
	switch {
	case len(members) == 0 && group != "":
		line += " " + dimStyle.Render("(empty)")
	case collapsed:
		line += " " + dimStyle.Render(fmt.Sprint(len(members)))
	}
	if open > 0 {
		line += " " + openStyle.Render(fmt.Sprintf("●%d", open))
	}
	if unseen {
		line += " " + unseenStyle.Render(unseenMarker)
	}
	return line
}

// connLabel is a Connection row after the cursor pointer, indented under its
// group header when the list is grouped: a dot for its Sessions (green ●
// open, yellow ● all ended, dim ○ not open), its alias, ⇄ for a running
// tunnel, • for unseen activity, then its description, in w columns. A
// path, Space › Group for a filter match from another Space, goes before
// the alias; when the row is too wide the description goes first, then the
// path is cut short, so the alias stays whole.
func connLabel(c *Connection, grouped bool, path string, tunnel bool, w int) string {
	dot, name := dimStyle.Render("○"), c.Alias
	switch {
	case c.AllEnded():
		dot = endedStyle.Render("●")
	case c.Open():
		dot = openStyle.Render("●")
	case c.Orphaned:
		name = dimStyle.Render(c.Alias)
	}
	lead := dot + " "
	if grouped {
		lead = "  " + lead
	}
	if tunnel {
		name += " " + openStyle.Render(tunnelMarker)
	}
	if c.Unseen() {
		name += " " + unseenStyle.Render(unseenMarker)
	}
	other := path != ""
	if other {
		path += pathSep
		if room := w - ansi.StringWidth(lead+name); ansi.StringWidth(path) > room {
			short := ""
			if room >= 2 {
				short = ansi.Truncate(strings.TrimSuffix(path, " "), room-1, "…") + " "
			}
			path = short
		}
		name = dimStyle.Render(path) + name
	}
	line := lead + name
	extra := rowDetail(c, grouped || other)
	if room := w - ansi.StringWidth(line) - 1; extra != "" && (!other || room > 0) {
		if other {
			extra = ansi.Truncate(extra, room, "…")
		}
		line += " " + dimStyle.Render(extra)
	}
	return line
}

// pathSep separates the Space, Group and alias of a filter match from
// another Space: acme › prod › web01.
const pathSep = " › "

// rowDetail is a Connection's description, or (missing) when it is orphaned.
// The group is added only when the list shows it nowhere else: [group].
func rowDetail(c *Connection, grouped bool) string {
	if c.Orphaned {
		return "(missing)"
	}
	d := c.Description
	if !grouped && c.Group != "" {
		d = strings.TrimSpace(d + " [" + c.Group + "]")
	}
	return d
}

// A tabSpan is where a tab sits on the tab line: columns [start, end) of
// the pane, and the tab's index, or newTabSpan for [+].
type tabSpan struct{ start, end, index int }

// newTabSpan is the index of the [+] that opens a new tab.
const newTabSpan = -1

// newTabLabel ends the tab line, a mouse target for a new tab.
const newTabLabel = "[+]"

// tabBar renders the Active connection's tabs in width columns,
// [1 ssh ●] [2 provision ●] [+], scrolling so the current tab stays
// visible, and reports where each visible tab and the [+] sit.
func (m *Model) tabBar(width int) (string, []tabSpan) {
	c := m.st.Active()
	if c == nil || !c.Open() || width <= 0 {
		return strings.Repeat(" ", max(width, 0)), nil
	}
	labels := make([]string, len(c.Sessions))
	plain := make([]string, len(c.Sessions))
	for i, s := range c.Sessions {
		labels[i], plain[i] = tabLabel(s, i == c.Current)
	}
	// Grow a window of tabs around the current one while it fits, leaving
	// room for ‹ and › and for the [+].
	avail := width - 2 - len(newTabLabel) - 1
	first, last := c.Current, c.Current
	used := ansi.StringWidth(plain[c.Current])
	for {
		grew := false
		if last+1 < len(plain) && used+1+ansi.StringWidth(plain[last+1]) <= avail {
			last++
			used += 1 + ansi.StringWidth(plain[last])
			grew = true
		}
		if first > 0 && used+1+ansi.StringWidth(plain[first-1]) <= avail {
			first--
			used += 1 + ansi.StringWidth(plain[first])
			grew = true
		}
		if !grew {
			break
		}
	}
	var b strings.Builder
	var spans []tabSpan
	x := 0
	write := func(s string, w int) { b.WriteString(s); x += w }
	if first > 0 {
		write(dimStyle.Render("‹"), 1)
	} else {
		write(" ", 1)
	}
	for i := first; i <= last; i++ {
		if i > first {
			write(" ", 1)
		}
		lw := ansi.StringWidth(plain[i])
		spans = append(spans, tabSpan{x, x + lw, i})
		write(labels[i], lw)
	}
	if last < len(labels)-1 {
		write(dimStyle.Render("›"), 1)
	} else {
		write(" ", 1)
	}
	spans = append(spans, tabSpan{x, x + len(newTabLabel), newTabSpan})
	write(dimStyle.Render(newTabLabel), len(newTabLabel))
	return fit(b.String(), width), spans
}

// tabLabel is a tab, [1 ssh ●]: its number, what it runs and a dot, green
// while live and yellow once ended. Unseen activity makes it bold and adds
// •. The current tab is in reverse video. plain is the label unstyled.
func tabLabel(s *Session, current bool) (label, plain string) {
	what := "ssh"
	if s.Action != "" {
		what = string(s.Action)
	}
	head := fmt.Sprintf("[%d %s ", s.N, what)
	dot := openStyle.Render("●")
	if s.Ended {
		dot = endedStyle.Render("●")
	}
	unseen := s.Unseen && !s.Ended
	plain = head + "●"
	if unseen {
		plain += " " + unseenMarker
	}
	plain += "]"
	if current {
		return activeRow.Render(plain), plain
	}
	style := lipgloss.NewStyle()
	if unseen {
		style = unseenStyle
	}
	label = style.Render(head) + dot
	if unseen {
		label += " " + unseenStyle.Render(unseenMarker)
	}
	return label + style.Render("]"), plain
}

// renderPane is the pane's lines, w columns wide, for the main area of f:
// the header (when f has room for it), the tab line and the Session.
func (m *Model) renderPane(w int, f frameLayout) []string {
	h := f.mainH
	if fm := m.form(); fm != nil {
		return fm.render(w, h, m.spaceNames(), fm.groups(m.st), m.features.On(catalog.FeatureBootstrap), m.opts.CommandNames)
	}
	if p := m.picker(); p != nil {
		return p.render(m, w, h)
	}
	var lines []string
	if f.paneHeader {
		lines = append(lines, fit(m.paneTitle(), w))
	}
	bar, _ := m.tabBar(w)
	lines = append(lines, bar)
	top := len(lines)
	var body []string
	s, r := m.currentRunner()
	switch {
	case s == nil && m.st.Filter() == "" && m.st.NoConnections():
		body = m.gettingStarted(w)
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
		if len(lines) >= h {
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
	lines = lines[:h]
	if s != nil && s.Ended && h > top {
		banner := fmt.Sprintf(" session ended (exit %d) · %s ", s.ExitCode, m.endedHints())
		lines[h-1] = endedBanner.Render(fit(banner, w))
	}
	return lines
}

// gettingStarted is the pane's body, w columns wide, while the active Space
// has no Connections and no filter is typed: how to add one, where Hosts
// already in ssh_config come from, and the Space picker and help keys.
func (m *Model) gettingStarted(w int) []string {
	title, where := "No connections yet.", "here"
	if m.st.Space() != "" {
		title = fmt.Sprintf("No connections in %s yet.", m.st.ActiveSpaceName())
		where = "in Space " + m.st.SpaceName("")
	}
	add, spaces, help := m.gettingStartedHints()
	paras := []string{
		add.String() + ": a new connection, saved to ~/.ssh/" + openssh.ManagedFile + ".",
		"Hosts already in ~/.ssh/config show up " + where + ". Saving needs " + openssh.IncludeLine +
			" at the top of ~/.ssh/config; sshctl asks before adding it.",
		spaces.String() + ": switch, add or rename Spaces.",
		help.String() + ": every key.",
	}
	body := []string{"", "  " + boldStyle.Render(title)}
	for _, p := range paras {
		body = append(body, "")
		for _, l := range strings.Split(ansi.Wordwrap(p, max(1, w-4), ""), "\n") {
			body = append(body, "  "+l)
		}
	}
	return body
}

// gettingStartedHints are the keys the getting-started screen names: add,
// the Space picker and help.
func (m *Model) gettingStartedHints() (add, spaces, help hintLine) {
	return m.hint(inList, "a"), m.hint(inList, "P"), m.hint(inPrefix, "?")
}

// paneTitle is the pane's header, alias · user@host[:port], or the alias
// alone until (or unless) its endpoint resolves.
func (m *Model) paneTitle() string {
	c := m.st.Active()
	if c == nil {
		return ""
	}
	t := " " + boldStyle.Render(c.Alias)
	if e := m.endpoints[c.Alias]; e.text != "" {
		t += dimStyle.Render(" · " + e.text)
	}
	return t
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

// statusHints are the hint lines for the status line's right side, for the
// focused pane's most important keys, the longest first; it shows the first
// that fits. Each drops the last key but help from the one before.
func (m *Model) statusHints() []hintLine {
	var keys hintLine
	if m.focus == focusPane && m.st.Active() != nil {
		keys = m.hint(inPrefix, "tab", "n", "w", "[", "]", "s")
	} else {
		keys = m.hint(inList, "enter", "/", "a", "e", "d")
	}
	help := m.hint(inPrefix, "?")
	var out []hintLine
	for n := len(keys.items); n >= 0; n-- {
		out = append(out, keys.first(n).and(help))
	}
	return out
}

// endedHints is the ended tab banner's hint line.
func (m *Model) endedHints() hintLine { return m.hint(inEnded, "r", "x") }

// sessionStatus is the status line's left side: the Active connection as
// Group / alias, and the key it uses.
func (m *Model) sessionStatus() string {
	key := "OpenSSH default"
	if k := m.identities.SpaceIdentity(m.st.Space()); k != "" {
		key = filepath.Base(k)
	}
	lead := ""
	if n := m.tunnels.count(); n > 0 {
		lead = tunnelMarker + " " + plural(n, "tunnel") + " · "
	}
	c := m.st.Active()
	if c == nil {
		return lead + "no active connection" + dimStyle.Render(" · key: "+key)
	}
	group := c.Group
	if group == "" {
		group = "ungrouped"
	}
	return lead + group + " / " + c.Alias + dimStyle.Render(" · key: "+key)
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
