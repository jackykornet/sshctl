package workspace

import (
	"fmt"
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
)

// unseenMarker flags tabs and Connections with activity the user hasn't seen.
const unseenMarker = "•"

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "sshctl"
	if s, r := m.currentRunner(); r != nil && !s.Ended && m.focus == focusPane &&
		!m.scrolling && m.modal == nil && !m.filtering && m.drag == nil {
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
	mainH := m.h - 1
	if _, ok := m.modal.(helpScreen); ok {
		// Help covers the whole Workspace without resizing the Sessions under it.
		lines := make([]string, 0, m.h)
		for _, l := range m.helpLines() {
			if len(lines) < mainH {
				lines = append(lines, fit(l, m.w))
			}
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
	rows, offset, top := m.listRows()
	lines := make([]string, 0, h)
	if top > 0 {
		f := "/" + m.st.Filter()
		if m.filtering {
			f += "█"
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
		pointer := " "
		if m.focus == focusList && offset+i == cursor {
			pointer = "›"
		}
		var line string
		highlight := false
		if r.Header() {
			line = pointer + m.headerLabel(r.Group)
			highlight = active != nil && active.Group == r.Group && m.st.Collapsed(r.Group) && m.st.Filter() == ""
		} else {
			line = pointer + connLabel(r.Conn, grouped)
			highlight = r.Conn == active
		}
		line = fit(line, w)
		switch {
		case m.drag != nil && m.drag.moving && !r.Header() && r.Conn.Alias == m.drag.alias:
			line = dimStyle.Render(ansi.Strip(line))
		case m.drag != nil && m.drag.moving && m.drag.ok && r.Group == m.drag.target:
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
	return lines
}

// headerLabel is a group header: its fold marker, name and member count, then
// how many members are open and whether any has unseen activity.
func (m *Model) headerLabel(group string) string {
	fold := "▾ "
	if m.st.Collapsed(group) && m.st.Filter() == "" {
		fold = "▸ "
	}
	name := boldStyle.Render(group)
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
	line := fold + name + " " + dimStyle.Render(fmt.Sprint(len(members)))
	if open > 0 {
		line += " " + openStyle.Render(fmt.Sprintf("●%d", open))
	}
	if unseen {
		line += " " + unseenStyle.Render(unseenMarker)
	}
	return line
}

// connLabel is a Connection row after the cursor pointer, indented under its
// group header when the list is grouped.
func connLabel(c *Connection, grouped bool) string {
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
	if c.Unseen() {
		line += " " + unseenStyle.Render(unseenMarker)
	}
	if extra := rowDetail(c, grouped); extra != "" {
		line += " " + dimStyle.Render(extra)
	}
	return line
}

// rowDetail is a Connection's description, or (missing) when it is orphaned.
// The group is added only when the list has no group headers to show it.
func rowDetail(c *Connection, grouped bool) string {
	if c.Orphaned {
		return "(missing)"
	}
	d := c.Description
	if c.Group != "" && !grouped {
		d = strings.TrimSpace(d + " [" + c.Group + "]")
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
		if s.Claude {
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
		return f.render(w, h)
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
	for _, l := range body {
		if len(lines) == h {
			break
		}
		// Reset after each line so remote styles never bleed into the separator.
		lines = append(lines, fit(l, w)+"\x1b[m")
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	if s != nil && s.Ended && h > 1 {
		banner := fmt.Sprintf(" session ended (exit %d) · r reconnect · x close ", s.ExitCode)
		lines[h-1] = endedBanner.Render(fit(banner, w))
	}
	return lines
}

// helpLines lists every binding (see bindings) with its keys and help text.
func (m *Model) helpLines() []string {
	labels := make([]string, len(bindings))
	width := 0
	for i, b := range bindings {
		labels[i] = m.bindingLabel(b)
		width = max(width, ansi.StringWidth(labels[i]))
	}
	lines := []string{"  " + boldStyle.Render("Keys") + dimStyle.Render("  (press any key to close)"), ""}
	for i, b := range bindings {
		pad := strings.Repeat(" ", width-ansi.StringWidth(labels[i]))
		lines = append(lines, "  "+labels[i]+pad+"  "+dimStyle.Render(b.help))
	}
	return lines
}

func (m *Model) renderStatus(w int) string {
	var left string
	modalStatus, ok := "", false
	if m.modal != nil {
		modalStatus, ok = m.modal.status()
	}
	switch {
	case ok:
		left = modalStatus
	case m.drag != nil && m.drag.moving:
		left = "drop " + m.drag.alias + " on a group · release off the list to cancel"
		if m.drag.ok {
			to := m.drag.target
			if to == "" {
				to = "ungrouped"
			}
			left = fmt.Sprintf("move %s → %s · release to drop", m.drag.alias, to)
		}
	case m.status != "" && m.statusErr:
		left = errStyle.Render(m.status)
	case m.status != "":
		left = m.status
	case m.scrolling:
		left = "scroll mode · ↑/↓ PgUp/PgDn g/G · q exit"
		if _, r := m.currentRunner(); r != nil {
			left = fmt.Sprintf("scroll: %d/%d lines back · ↑/↓ PgUp/PgDn g/G · q exit", r.scroll, r.term.ScrollbackLen())
		}
	default:
		left = m.sessionStatus()
	}
	if m.prefix || m.modal != nil && m.modal.prefixArmed() {
		left = keyHint(m.opts.PrefixKey) + " … " + left
	}
	p := keyHint(m.opts.PrefixKey)
	line := " " + left
	for _, right := range []string{
		fmt.Sprintf("%s ? help · %s n new tab · %s w close", p, p, p),
		p + " ? help",
	} {
		if gap := w - ansi.StringWidth(line) - ansi.StringWidth(right) - 1; gap >= 1 {
			line += strings.Repeat(" ", gap) + right + " "
			break
		}
	}
	return barStyle.Render(fit(line, w))
}

func (m *Model) sessionStatus() string {
	key := "OpenSSH default"
	if m.identity != "" {
		key = filepath.Base(m.identity)
	}
	c := m.st.Active()
	if c == nil {
		return "no active connection · key: " + key
	}
	s := c.CurrentSession()
	state := "connected"
	if s.Ended {
		state = fmt.Sprintf("ended (exit %d)", s.ExitCode)
	}
	return fmt.Sprintf("%s · tab %d/%d · %s · key: %s", c.Alias, c.Current+1, len(c.Sessions), state, key)
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
