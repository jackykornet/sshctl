package workspace

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	defaultPrefixKey  = "ctrl+b"
	defaultListWidth  = 28
	defaultScrollback = 10000
	// autoHideWidth is the total width below which the connection list hides itself.
	autoHideWidth = 60
	statusTimeout = 4 * time.Second
	wheelLines    = 3
)

// Snapshot is what the Workspace loads at startup and on refresh.
type Snapshot struct {
	Entries []Entry
	// Identity is the Selected identity's key path, or "" when OpenSSH chooses keys.
	Identity string
}

// Options configures a Workspace.
type Options struct {
	Load func() (Snapshot, error)
	// Command returns the command line for a new Session of alias; claude asks
	// for a Session that runs claude on the remote host. It is called each time
	// a tab opens or reconnects, so it can use the Selected identity at that
	// moment. When nil, Sessions get no process (used by layout tests).
	Command func(alias string, claude bool) ([]string, error)
	// OnOpen is called when a Connection becomes open (its first Session starts).
	OnOpen          func(alias string) error
	PrefixKey       string
	ListWidth       int
	ScrollbackLines int
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
	ev        *events
	runners   map[*Session]*runner
	w, h      int
	focus     focus
	prefix    bool
	filtering bool
	scrolling bool
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
	if opts.ScrollbackLines <= 0 {
		opts.ScrollbackLines = defaultScrollback
	}
	snap, err := opts.Load()
	if err != nil {
		return nil, err
	}
	return &Model{
		opts:     opts,
		st:       NewState(snap.Entries),
		identity: snap.Identity,
		ev:       newEvents(),
		runners:  map[*Session]*runner{},
	}, nil
}

// Run starts the Workspace and blocks until it quits. Every Session still
// running when it quits is ended.
func Run(opts Options) error {
	m, err := New(opts)
	if err != nil {
		return err
	}
	defer m.closeAll()
	_, err = tea.NewProgram(m).Run()
	return err
}

func (m *Model) closeAll() {
	for s, r := range m.runners {
		r.close()
		delete(m.runners, s)
	}
}

func (m *Model) Init() tea.Cmd { return m.ev.listen() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.syncSize()
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case clearStatusMsg:
		if msg.id == m.statusID {
			m.status = ""
		}
	case redrawMsg:
		return m.ev.listen()
	case sessionEndedMsg:
		if m.runners[msg.sess] == msg.r { // ignore a runner replaced by reconnect
			msg.sess.Ended, msg.sess.ExitCode = true, msg.code
		}
		return m.ev.listen()
	case clipboardMsg:
		return tea.Batch(tea.SetClipboard(msg.text), m.ev.listen())
	case tea.PasteMsg:
		if r := m.liveRunner(); r != nil && m.focus == focusPane && m.confirm == nil {
			r.term.Paste(msg.Content)
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return nil
}

// paneSize is the size of the terminal area under the tab bar.
func (m *Model) paneSize() (int, int) {
	return max(m.w-m.paneOffset(), 1), max(m.h-2, 1)
}

// syncSize resizes every Session's terminal to the current pane size.
func (m *Model) syncSize() {
	if m.w == 0 {
		return
	}
	w, h := m.paneSize()
	for _, r := range m.runners {
		r.resize(w, h)
	}
}

// currentRunner returns the process behind the Active connection's current tab.
func (m *Model) currentRunner() (*Session, *runner) {
	c := m.st.Active()
	if c == nil {
		return nil, nil
	}
	s := c.CurrentSession()
	return s, m.runners[s]
}

func (m *Model) liveRunner() *runner {
	s, r := m.currentRunner()
	if s == nil || s.Ended {
		return nil
	}
	return r
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
		return m.prefixCommand(msg)
	}
	if k == m.opts.PrefixKey {
		m.prefix = true
		return nil
	}
	switch k {
	case "alt+left":
		m.selectTab(-1)
		return nil
	case "alt+right":
		m.selectTab(1)
		return nil
	case "alt+up":
		m.st.CycleActive(-1)
		m.scrolling = false
		return nil
	case "alt+down":
		m.st.CycleActive(1)
		m.scrolling = false
		return nil
	}
	if m.filtering {
		m.filterKey(msg)
		return nil
	}
	if m.focus == focusPane {
		return m.paneKey(msg)
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

// paneKey handles a key while the terminal pane has focus: scroll mode keys,
// the ended-Session actions, or forwarding to the live Session.
func (m *Model) paneKey(msg tea.KeyPressMsg) tea.Cmd {
	s, r := m.currentRunner()
	if s == nil {
		return nil
	}
	if m.scrolling && r != nil {
		m.scrollKey(r, msg.String())
		return nil
	}
	if s.Ended {
		switch msg.String() {
		case "r":
			return m.reconnect()
		case "x":
			return m.closeTab()
		}
		return nil
	}
	if r != nil {
		r.scroll = 0
		r.term.SendKey(msg)
	}
	return nil
}

func (m *Model) scrollKey(r *runner, k string) {
	_, h := m.paneSize()
	switch k {
	case "up", "k":
		m.scrollBy(r, 1)
	case "down", "j":
		m.scrollBy(r, -1)
	case "pgup":
		m.scrollBy(r, h)
	case "pgdown", "ctrl+f", "space":
		m.scrollBy(r, -h)
	case "g", "home":
		m.scrollBy(r, r.term.ScrollbackLen())
	case "G", "end", "q", "esc":
		r.scroll, m.scrolling = 0, false
	}
}

func (m *Model) scrollBy(r *runner, n int) {
	r.scroll = max(0, min(r.scroll+n, r.term.ScrollbackLen()))
	m.scrolling = r.scroll > 0
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

func (m *Model) prefixCommand(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	switch k {
	case m.opts.PrefixKey:
		if r := m.liveRunner(); r != nil && m.focus == focusPane {
			r.term.SendKey(msg)
		}
	case "n", "c":
		target := m.st.Active()
		if target == nil {
			target = m.st.Cursor()
		}
		if target != nil {
			return m.open(target.Alias, k == "c")
		}
	case "w":
		return m.closeTab()
	case "left":
		m.selectTab(-1)
	case "right":
		m.selectTab(1)
	case "[":
		if _, r := m.currentRunner(); r != nil {
			m.scrolling = true
			m.focus = focusPane
		}
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
		m.scrolling = false
	default:
		return m.setStatus(fmt.Sprintf("unknown command %s %s (%s ? for help)", keyHint(m.opts.PrefixKey), k, keyHint(m.opts.PrefixKey)), true)
	}
	return nil
}

func (m *Model) selectTab(delta int) {
	m.st.SelectTab(delta)
	m.scrolling = false
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
	return m.open(c.Alias, false)
}

// open starts a new Session (tab) for alias, running claude when claude is set.
func (m *Model) open(alias string, claude bool) tea.Cmd {
	c := m.st.Get(alias)
	first := c != nil && !c.Open()
	sess, err := m.st.OpenSession(alias)
	if err != nil {
		if errors.Is(err, ErrOrphaned) {
			return m.setStatus(alias+" is missing from ssh_config", true)
		}
		return m.setStatus(err.Error(), true)
	}
	sess.Claude = claude
	if err := m.start(alias, sess); err != nil {
		m.st.CloseCurrent()
		if m.st.Active() == nil {
			m.focus = focusList
		}
		return m.setStatus(alias+": "+err.Error(), true)
	}
	m.focus, m.scrolling = focusPane, false
	if first && m.opts.OnOpen != nil {
		if err := m.opts.OnOpen(alias); err != nil {
			return m.setStatus("history: "+err.Error(), true)
		}
	}
	return nil
}

// start runs the Session's process, replacing any previous one.
func (m *Model) start(alias string, sess *Session) error {
	if m.opts.Command == nil {
		return nil
	}
	argv, err := m.opts.Command(alias, sess.Claude)
	if err != nil {
		return err
	}
	w, h := m.paneSize()
	r, err := startRunner(argv, w, h, m.opts.ScrollbackLines, sess, m.ev)
	if err != nil {
		return err
	}
	if old := m.runners[sess]; old != nil {
		old.close()
	}
	m.runners[sess] = r
	return nil
}

// reconnect starts a new process in the current tab after its Session ended.
func (m *Model) reconnect() tea.Cmd {
	c := m.st.Active()
	if c == nil {
		return nil
	}
	sess := c.CurrentSession()
	if !sess.Ended {
		return nil
	}
	if c.Orphaned {
		return m.setStatus(c.Alias+" is missing from ssh_config", true)
	}
	if err := m.start(c.Alias, sess); err != nil {
		return m.setStatus(c.Alias+": "+err.Error(), true)
	}
	sess.Ended, sess.ExitCode = false, 0
	m.scrolling = false
	return nil
}

func (m *Model) closeTab() tea.Cmd {
	c := m.st.Active()
	if c == nil {
		return nil
	}
	sess := c.CurrentSession()
	doClose := func(m *Model) tea.Cmd {
		if s := m.st.CloseCurrent(); s != nil {
			if r := m.runners[s]; r != nil {
				r.close()
				delete(m.runners, s)
			}
		}
		m.scrolling = false
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

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	mm := msg.Mouse()
	if mm.Y >= m.h-1 || m.help || m.confirm != nil {
		return nil
	}
	lw := m.listWidth()
	if mm.X < lw {
		return m.listMouse(msg, mm)
	}
	paneX := mm.X - m.paneOffset()
	if paneX < 0 {
		return nil // the separator column
	}
	if mm.Y == 0 {
		if click, ok := msg.(tea.MouseClickMsg); ok && click.Button == tea.MouseLeft {
			_, spans := m.tabBar(m.w - m.paneOffset())
			for _, sp := range spans {
				if paneX >= sp.start && paneX < sp.end {
					m.st.SelectTabIndex(sp.index)
					m.scrolling = false
				}
			}
			if m.st.Active() != nil {
				m.focus = focusPane
			}
		}
		return nil
	}
	s, r := m.currentRunner()
	if s == nil || r == nil {
		return nil
	}
	if _, ok := msg.(tea.MouseClickMsg); ok {
		m.focus = focusPane
	}
	// Full-screen programs get the mouse; otherwise the wheel scrolls history.
	if r.term.AltScreen() && !s.Ended {
		r.term.SendMouse(msg, paneX, mm.Y-1)
		return nil
	}
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		switch mm.Button {
		case tea.MouseWheelUp:
			m.scrollBy(r, wheelLines)
		case tea.MouseWheelDown:
			m.scrollBy(r, -wheelLines)
		}
	}
	return nil
}

func (m *Model) listMouse(msg tea.MouseMsg, mm tea.Mouse) tea.Cmd {
	switch msg.(type) {
	case tea.MouseWheelMsg:
		switch mm.Button {
		case tea.MouseWheelUp:
			m.st.MoveCursor(-1)
		case tea.MouseWheelDown:
			m.st.MoveCursor(1)
		}
	case tea.MouseClickMsg:
		if mm.Button != tea.MouseLeft {
			return nil
		}
		rows, offset, top := m.listRows()
		i := mm.Y - top + offset
		if mm.Y < top || i >= len(rows) {
			return nil
		}
		c := rows[i]
		if c.Open() {
			m.st.Activate(c.Alias)
			m.focus, m.scrolling = focusPane, false
		} else {
			m.st.SetCursor(c.Alias)
			m.focus = focusList
		}
	}
	return nil
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
