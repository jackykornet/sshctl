package workspace

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	defaultPrefixKey = "ctrl+b"
	defaultListWidth = 28
	// autoHideWidth is the total width below which the connection list hides itself.
	autoHideWidth = 60
	statusTimeout = 4 * time.Second
)

// Snapshot is what the Workspace loads at startup and on refresh.
type Snapshot struct {
	Entries []Entry
	// Identity is the Selected identity's key path, or "" when OpenSSH chooses keys.
	Identity string
}

// Options configures a Workspace.
type Options struct {
	Load      func() (Snapshot, error)
	PrefixKey string
	ListWidth int
}

type focus int

const (
	focusList focus = iota
	focusPane
)

type confirmation struct {
	question string
	yes      func(*Model) tea.Cmd
}

type clearStatusMsg struct{ id int }

// Model is the Workspace's Bubble Tea model.
type Model struct {
	opts      Options
	st        *State
	identity  string
	w, h      int
	focus     focus
	prefix    bool
	filtering bool
	hideList  bool
	help      bool
	confirm   *confirmation
	status    string
	statusErr bool
	statusID  int
}

// New loads the first Snapshot and builds the Workspace model.
func New(opts Options) (*Model, error) {
	if opts.PrefixKey == "" {
		opts.PrefixKey = defaultPrefixKey
	}
	if opts.ListWidth <= 0 {
		opts.ListWidth = defaultListWidth
	}
	snap, err := opts.Load()
	if err != nil {
		return nil, err
	}
	return &Model{opts: opts, st: NewState(snap.Entries), identity: snap.Identity}, nil
}

// Run starts the Workspace and blocks until it quits.
func Run(opts Options) error {
	m, err := New(opts)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m).Run()
	return err
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case clearStatusMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
	case tea.KeyPressMsg:
		return m, m.key(msg)
	case tea.MouseClickMsg:
		return m, m.click(msg.Mouse())
	case tea.MouseWheelMsg:
		if mm := msg.Mouse(); mm.X < m.listWidth() {
			switch mm.Button {
			case tea.MouseWheelUp:
				m.st.MoveCursor(-1)
			case tea.MouseWheelDown:
				m.st.MoveCursor(1)
			}
		}
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if c := m.confirm; c != nil {
		m.confirm = nil
		if k == "y" || k == "Y" {
			return c.yes(m)
		}
		return nil
	}
	if m.help {
		m.help = false
		return nil
	}
	if m.prefix {
		m.prefix = false
		return m.prefixCommand(k)
	}
	if k == m.opts.PrefixKey {
		m.prefix = true
		return nil
	}
	switch k {
	case "alt+left":
		m.st.SelectTab(-1)
		return nil
	case "alt+right":
		m.st.SelectTab(1)
		return nil
	case "alt+up":
		m.st.CycleActive(-1)
		return nil
	case "alt+down":
		m.st.CycleActive(1)
		return nil
	}
	if m.filtering {
		m.filterKey(msg)
		return nil
	}
	if m.focus == focusPane {
		// Key presses are forwarded to the Session once Sessions run ssh.
		return nil
	}
	switch k {
	case "up", "k":
		m.st.MoveCursor(-1)
	case "down", "j":
		m.st.MoveCursor(1)
	case "enter":
		return m.enter()
	case "/":
		m.filtering = true
	case "esc":
		m.st.SetFilter("")
	case "q", "ctrl+c":
		return m.quit()
	}
	return nil
}

func (m *Model) filterKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.st.SetFilter("")
	case "enter":
		m.filtering = false
	case "backspace":
		f := []rune(m.st.Filter())
		if len(f) > 0 {
			m.st.SetFilter(string(f[:len(f)-1]))
		}
	default:
		if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
			m.st.SetFilter(m.st.Filter() + msg.Text)
		}
	}
}

func (m *Model) prefixCommand(k string) tea.Cmd {
	switch k {
	case m.opts.PrefixKey:
		// Sends a literal prefix key to the Session once Sessions run ssh.
	case "n":
		target := m.st.Active()
		if target == nil {
			target = m.st.Cursor()
		}
		if target != nil {
			return m.open(target.Alias)
		}
	case "w":
		return m.closeTab()
	case "left":
		m.st.SelectTab(-1)
	case "right":
		m.st.SelectTab(1)
	case "tab":
		if m.focus == focusList && m.st.Active() != nil {
			m.focus = focusPane
		} else {
			m.focus = focusList
		}
	case "b":
		m.hideList = !m.hideList
	case "r":
		return m.refresh()
	case "?":
		m.help = true
	case "q":
		return m.quit()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		m.st.SelectTabIndex(int(k[0] - '1'))
	default:
		return m.setStatus(fmt.Sprintf("unknown command %s %s (%s ? for help)", keyHint(m.opts.PrefixKey), k, keyHint(m.opts.PrefixKey)), true)
	}
	return nil
}

func (m *Model) enter() tea.Cmd {
	c := m.st.Cursor()
	if c == nil {
		return nil
	}
	if c.Open() {
		m.st.Activate(c.Alias)
		m.focus = focusPane
		return nil
	}
	return m.open(c.Alias)
}

func (m *Model) open(alias string) tea.Cmd {
	if _, err := m.st.OpenSession(alias); err != nil {
		if errors.Is(err, ErrOrphaned) {
			return m.setStatus(alias+" is missing from ssh_config", true)
		}
		return m.setStatus(err.Error(), true)
	}
	m.focus = focusPane
	return nil
}

func (m *Model) closeTab() tea.Cmd {
	c := m.st.Active()
	if c == nil {
		return nil
	}
	sess := c.CurrentSession()
	doClose := func(m *Model) tea.Cmd {
		m.st.CloseCurrent()
		if m.st.Active() == nil {
			m.focus = focusList
		}
		return nil
	}
	if sess.Ended {
		return doClose(m)
	}
	m.confirm = &confirmation{question: fmt.Sprintf("Close %s %d? (y/n)", c.Alias, sess.N), yes: doClose}
	return nil
}

func (m *Model) quit() tea.Cmd {
	n := m.st.LiveSessions()
	if n == 0 {
		return tea.Quit
	}
	noun := "sessions"
	if n == 1 {
		noun = "session"
	}
	m.confirm = &confirmation{
		question: fmt.Sprintf("%d live %s will be closed. Quit? (y/n)", n, noun),
		yes:      func(*Model) tea.Cmd { return tea.Quit },
	}
	return nil
}

func (m *Model) refresh() tea.Cmd {
	snap, err := m.opts.Load()
	if err != nil {
		return m.setStatus("refresh: "+err.Error(), true)
	}
	m.st.Reload(snap.Entries)
	m.identity = snap.Identity
	return m.setStatus("connection list refreshed", false)
}

func (m *Model) setStatus(s string, isErr bool) tea.Cmd {
	m.statusID++
	m.status, m.statusErr = s, isErr
	id := m.statusID
	return tea.Tick(statusTimeout, func(time.Time) tea.Msg { return clearStatusMsg{id} })
}

func (m *Model) click(mm tea.Mouse) tea.Cmd {
	if mm.Button != tea.MouseLeft || mm.Y >= m.h-1 {
		return nil
	}
	lw := m.listWidth()
	if mm.X < lw {
		rows, offset, top := m.listRows()
		i := mm.Y - top + offset
		if mm.Y < top || i >= len(rows) {
			return nil
		}
		c := rows[i]
		if c.Open() {
			m.st.Activate(c.Alias)
			m.focus = focusPane
		} else {
			m.st.SetCursor(c.Alias)
			m.focus = focusList
		}
		return nil
	}
	paneX := mm.X - lw
	if lw > 0 {
		paneX--
	}
	if mm.Y == 0 {
		_, spans := m.tabBar(m.w - m.paneOffset())
		for _, sp := range spans {
			if paneX >= sp.start && paneX < sp.end {
				m.st.SelectTabIndex(sp.index)
			}
		}
	}
	if m.st.Active() != nil {
		m.focus = focusPane
	}
	return nil
}

// listWidth is the connection list's width, or 0 when it is hidden.
func (m *Model) listWidth() int {
	if m.hideList || m.help || m.w < autoHideWidth {
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
// screen row where they start (after the filter line, if shown).
func (m *Model) listRows() (rows []*Connection, offset, top int) {
	rows = m.st.Rows()
	if m.filtering || m.st.Filter() != "" {
		top = 1
	}
	height := m.h - 1 - top
	cur := m.st.index(rows, m.st.cursor)
	if height > 0 && cur >= height {
		offset = cur - height + 1
	}
	return rows, offset, top
}

func keyHint(k string) string {
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok && len(rest) == 1 {
		return "^" + strings.ToUpper(rest)
	}
	return k
}
