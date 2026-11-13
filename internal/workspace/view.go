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
)

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "sshctl"
	if s, r := m.currentRunner(); r != nil && !s.Ended && m.focus == focusPane &&
		!m.scrolling && !m.help && m.confirm == nil && !m.filtering {
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
	if m.help {
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
	cursor := m.st.Cursor()
	active := m.st.Active()
	for _, c := range rows[min(offset, len(rows)):] {
		if len(lines) == h {
			break
		}
		pointer := " "
		if m.focus == focusList && c == cursor {
			pointer = "›"
		}
		marker, name := "  ", c.Alias
		switch {
		case c.AllEnded():
			marker, name = endedStyle.Render("● "), endedStyle.Render(c.Alias)
		case c.Open():
			marker, name = openStyle.Render("● "), openStyle.Render(c.Alias)
		case c.Orphaned:
			name = dimStyle.Render(c.Alias)
		}
		line := pointer + marker + name
		if extra := rowDetail(c); extra != "" {
			line += " " + dimStyle.Render(extra)
		}
		line = fit(line, w)
		if c == active {
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

func rowDetail(c *Connection) string {
	if c.Orphaned {
		return "(missing)"
	}
	d := c.Description
	if c.Group != "" {
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
	for i, s := range c.Sessions {
		label := fmt.Sprintf("%s %d", c.Alias, s.N)
		if s.Claude {
			label += " claude"
		}
		if s.Ended {
			label += " ✕"
		}
		labels[i] = " " + label + " "
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
		switch {
		case i == c.Current:
			b.WriteString(activeRow.Render(labels[i]))
		case c.Sessions[i].Ended:
			b.WriteString(endedStyle.Render(labels[i]))
		default:
			b.WriteString(dimStyle.Render(labels[i]))
		}
		x += lw
	}
	if last < len(labels)-1 {
		b.WriteString(dimStyle.Render("›"))
	}
	return fit(b.String(), width), spans
}

func (m *Model) renderPane(w, h int) []string {
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

func (m *Model) helpLines() []string {
	p := keyHint(m.opts.PrefixKey)
	rows := [][2]string{
		{"↑/↓ Enter", "move, open or switch to a connection"},
		{"/", "filter the connection list (Esc clears)"},
		{"Alt+←/→", "previous / next tab"},
		{"Alt+↑/↓", "previous / next open connection"},
		{p + " n", "new tab for the active connection"},
		{p + " c", "new tab running claude on the remote host"},
		{p + " g", "open the connection's group as tabs"},
		{p + " w", "close tab"},
		{p + " ←/→  " + p + " 1-9", "switch tab"},
		{p + " Tab", "move focus between list and terminal"},
		{p + " [", "scroll back (mouse wheel also scrolls)"},
		{"r / x", "on an ended tab: reconnect / close"},
		{p + " b", "hide or show the connection list"},
		{p + " r", "refresh connections from ssh_config"},
		{p + " " + p, "send " + p + " to the session"},
		{p + " q", "quit"},
	}
	lines := []string{"", "  " + boldStyle.Render("Keys") + dimStyle.Render("  (press any key to close)"), ""}
	for _, r := range rows {
		lines = append(lines, fmt.Sprintf("  %-14s %s", r[0], dimStyle.Render(r[1])))
	}
	return lines
}

func (m *Model) renderStatus(w int) string {
	var left string
	switch {
	case m.confirm != nil:
		left = m.confirm.question
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
	if m.prefix {
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
