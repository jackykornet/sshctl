package workspace

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
)

const (
	defaultPrefixKey  = "ctrl+b"
	defaultListWidth  = 28
	defaultScrollback = 10000
	// autoHideWidth is the total width below which the connection list hides itself.
	autoHideWidth = 60
	statusTimeout = 4 * time.Second
	// spaceNoteTimeout is how long the Space note stays without a key press.
	spaceNoteTimeout = 3 * time.Second
	wheelLines       = 3
)

// Snapshot is what the Workspace loads at startup and on refresh. Its
// Collapsed groups apply at startup only.
type Snapshot = catalog.Snapshot

// Catalog is the Connection catalog as the Workspace uses it: everything the
// Workspace reads and saves goes through it. The real one is
// *catalog.Catalog.
type Catalog interface {
	// Snapshot reads the Connection list fresh from disk.
	Snapshot() (Snapshot, error)
	// RecordOpen records in History that alias was opened.
	RecordOpen(alias string) error
	// SetGroup saves alias's group ("" for none).
	SetGroup(alias, group string) error
	// SetCollapsed saves every group the list shows collapsed.
	SetCollapsed(groups []string) error
	// Save adds a Connection (old is "") or saves changes to old. It returns
	// an *IncludeError when ssh_config must first read sshctl's hosts file.
	Save(old string, s Spec) error
	// Delete deletes alias's Host block when sshctl manages it, and its
	// sshctl metadata.
	Delete(alias string) error
	// AddInclude makes ssh_config read sshctl's hosts file.
	AddInclude() error
	// The Group methods take the Space by name, "" for the default Space.

	// MoveGroup puts space's Group name at position among space's Groups,
	// counting from 1.
	MoveGroup(space, name string, position int) error
	// AddGroup creates an empty Group after space's others.
	AddGroup(space, name string) error
	// RenameGroup renames a Group; its members move with it.
	RenameGroup(space, old, name string) error
	// DeleteGroup deletes a Group; its members become ungrouped.
	DeleteGroup(space, name string) error
	// SetActiveSpace saves the Space the Workspace shows, by name ("" for
	// the default Space).
	SetActiveSpace(space string) error
	// AddSpace creates an empty Space after the others.
	AddSpace(name string) error
	// RenameSpace renames a Space; its Groups and Connections, collapsed
	// Groups and whether it is active move with it.
	RenameSpace(old, name string) error
	// DeleteSpace deletes an empty Space other than the default Space.
	DeleteSpace(name string) error
	// MoveSpace puts a Space at position, counting from 1.
	MoveSpace(name string, position int) error
	// ProvisionRepo returns the dotfiles repo Provision applies to alias,
	// catalog.ErrNoDotfiles when none is set, or an error wrapping
	// catalog.ErrProvisionRefused when the setting that applies is none.
	ProvisionRepo(alias string) (string, error)
}

// Options configures a Workspace.
type Options struct {
	// Catalog loads and saves Connections and the Workspace's saved state.
	// It is required.
	Catalog Catalog
	// Command returns the command line for a new Session of alias that runs
	// what l says: ssh, claude on the remote host, or one of sshctl's own
	// Actions. It is called each time a tab opens or reconnects, so it can
	// use the Selected identity at that moment. When nil, Sessions get no
	// process (used by layout tests).
	Command         func(alias string, l Launch) ([]string, error)
	PrefixKey       string
	ListWidth       int
	ScrollbackLines int
	// Notice is shown on the status line at startup, such as what loading
	// the config migrated.
	Notice string
}

type focus int

const (
	focusList focus = iota
	focusPane
)

type clearStatusMsg struct{ id int }

// hideSpaceNoteMsg hides the Space note shown with the same id.
type hideSpaceNoteMsg struct{ id int }

// drag is a Connection, or a Group header when group is set, held down with
// the left mouse button in the list.
type drag struct {
	alias string
	group string
	fromY int
	// moving is set once the pointer leaves the row it was pressed on.
	moving bool
	// target is the group under the pointer; ok is false off the list, and
	// for a header drag anywhere but another stored Group's header.
	target string
	ok     bool
}

// Model is the Workspace's Bubble Tea model.
type Model struct {
	opts      Options
	st        *State
	identity  string
	ev        *events
	sessions  *sessions
	w, h      int
	focus     focus
	prefix    bool // armed outside modals; a modal keeps its own
	filtering bool
	// filter is the filter being typed, with its cursor; State holds its text.
	filter    line
	scrolling bool
	hideList  bool
	modal     modal // gets input before anything else; see modal
	drag      *drag
	status    string
	statusErr bool
	statusID  int
	// spaceNote shows every Space's number and name under the Space bar
	// until the next key, or the hideSpaceNoteMsg carrying spaceNoteID.
	spaceNote   bool
	spaceNoteID int
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
	if opts.Catalog == nil {
		return nil, errors.New("workspace: no Connection catalog")
	}
	snap, err := opts.Catalog.Snapshot()
	if err != nil {
		return nil, err
	}
	st := NewState(snap.Entries)
	st.SetSpaces(snap.Spaces, snap.ActiveSpace)
	st.SetGroups(snap.Groups)
	st.SetSpace(snap.ActiveSpace)
	st.RestoreCollapsed(snap.Collapsed)
	m := &Model{opts: opts, st: st, identity: snap.Identity, ev: newEvents()}
	m.sessions = &sessions{
		st:         st,
		ev:         m.ev,
		command:    opts.Command,
		scrollback: opts.ScrollbackLines,
		size:       m.paneSize,
		runners:    map[*Session]*runner{},
	}
	if opts.Notice != "" {
		m.status = opts.Notice
	}
	if snap.Include != nil {
		m.offerInclude(snap.Include)
	}
	return m, nil
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

func (m *Model) closeAll() { m.sessions.closeAll() }

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
	case hideSpaceNoteMsg:
		if msg.id == m.spaceNoteID {
			m.spaceNote = false
		}
	case redrawMsg:
		m.sessions.takeActivity()
		return m.ev.listen()
	case sessionEndedMsg:
		m.sessions.takeActivity() // its last output may not have been redrawn yet
		if m.sessions.ended(msg) && msg.sess.Action != "" && msg.code == 0 {
			// An Action may have rewritten the Connection's Host block.
			return tea.Batch(m.reload("", string(msg.sess.Action)+" finished; connection list refreshed"), m.ev.listen())
		}
		return m.ev.listen()
	case clipboardMsg:
		return tea.Batch(tea.SetClipboard(msg.text), m.ev.listen())
	case tea.PasteMsg:
		m.paste(msg.Content)
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
	m.sessions.resize(m.paneSize())
}

// currentRunner returns the process behind the Active connection's current tab.
func (m *Model) currentRunner() (*Session, *runner) {
	c := m.st.Active()
	if c == nil {
		return nil, nil
	}
	s := c.CurrentSession()
	return s, m.sessions.runner(s)
}

func (m *Model) liveRunner() *runner {
	s, r := m.currentRunner()
	if s == nil || s.Ended {
		return nil
	}
	return r
}

// paste sends pasted text to the modal, else the filter being typed, else
// the live Session when the terminal has focus.
func (m *Model) paste(text string) {
	switch r := m.liveRunner(); {
	case m.modal != nil:
		m.modal.paste(text)
	case m.filtering:
		m.filter.paste(text)
		m.st.SetFilter(m.filter.String())
	case r != nil && m.focus == focusPane:
		r.term.Paste(text)
	}
}

// key routes a key press: to the modal when one is open, then through the
// bindings for the context the Workspace is in (see bindings).
func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	m.spaceNote = false
	if m.modal != nil {
		return m.modal.key(m, msg)
	}
	if m.prefix {
		m.prefix = false
		return m.prefixCommand(msg)
	}
	if msg.String() == m.opts.PrefixKey {
		m.prefix = true
		return nil
	}
	if cmd, ok := m.runBinding(inAny, msg); ok {
		return cmd
	}
	if m.filtering {
		m.filterKey(msg)
		return nil
	}
	if m.focus == focusPane {
		return m.paneKey(msg)
	}
	cmd, _ := m.runBinding(inList, msg)
	return cmd
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
		cmd, _ := m.runBinding(inEnded, msg)
		return cmd
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

// startFilter starts typing the filter, resuming the current one with the
// cursor at its end.
func (m *Model) startFilter() {
	m.filtering = true
	m.filter = newLine(m.st.Filter())
}

func (m *Model) filterKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.st.SetFilter("")
	case "enter":
		m.filtering = false
	default:
		if m.filter.key(msg) {
			m.st.SetFilter(m.filter.String())
		}
	}
}

func (m *Model) prefixCommand(msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := m.runBinding(inPrefix, msg); ok {
		return cmd
	}
	p := keyHint(m.opts.PrefixKey)
	return m.setStatus(fmt.Sprintf("unknown command %s %s (%s ? for help)", p, msg.String(), p), true)
}

func (m *Model) selectTab(delta int) {
	m.st.SelectTab(delta)
	m.scrolling = false
}

func (m *Model) enter() tea.Cmd {
	if r, ok := m.st.CursorRow(); ok && r.Header() {
		return m.setCollapsed(r.Group, !m.st.Collapsed(r.Group))
	}
	c := m.st.Cursor()
	if c == nil {
		return nil
	}
	if c.Open() {
		m.st.Activate(c.Alias)
		m.focus = focusPane
		return nil
	}
	return m.open(c.Alias, Launch{})
}

// open starts a new Session (tab) for alias running what l says, and shows
// any error on the status line.
func (m *Model) open(alias string, l Launch) tea.Cmd {
	if err := m.openSession(alias, l); err != nil {
		return m.setStatus(err.Error(), true)
	}
	return nil
}

// openSession starts a new Session (tab) for alias. When the Session cannot
// start, the Connection is left as it was and the error is a *startError.
// A History error is returned with the Session left running.
func (m *Model) openSession(alias string, l Launch) error {
	c := m.st.Get(alias)
	first := c != nil && !c.Open()
	if _, err := m.sessions.open(alias, l); err != nil {
		if errors.Is(err, ErrOrphaned) {
			return errors.New(alias + " is missing from ssh_config")
		}
		if m.st.Active() == nil {
			m.focus = focusList
		}
		return err
	}
	m.focus, m.scrolling = focusPane, false
	if first {
		if err := m.opts.Catalog.RecordOpen(alias); err != nil {
			return fmt.Errorf("history: %w", err)
		}
	}
	return nil
}

// startError is a Session that could not start for alias.
type startError struct {
	alias string
	err   error
}

func (e *startError) Error() string { return e.alias + ": " + e.err.Error() }
func (e *startError) Unwrap() error { return e.err }

// openGroup opens a Session for every Connection State.GroupToOpen returns
// for the cursor Connection's group and makes the first one active. The
// status reports every member that failed to start.
func (m *Model) openGroup() tea.Cmd {
	row, ok := m.st.CursorRow()
	group := row.Group
	if !ok || group == "" {
		return m.setStatus("no group to open", false)
	}
	members := m.st.GroupToOpen(group)
	if len(members) == 0 {
		return m.setStatus("nothing to open in group "+group, false)
	}
	var opened []string
	var failed []*startError
	var other error // e.g. a History error; the Session still opened
	for _, member := range members {
		err := m.openSession(member.Alias, Launch{})
		var se *startError
		switch {
		case errors.As(err, &se):
			failed = append(failed, se)
		case err != nil:
			opened = append(opened, member.Alias)
			other = err
		default:
			opened = append(opened, member.Alias)
		}
	}
	if len(opened) > 0 {
		m.st.Activate(opened[0])
	}
	if len(failed) == 0 {
		if other != nil {
			return m.setStatus(other.Error(), true)
		}
		return m.setStatus(fmt.Sprintf("opened %s in group %s", countConnections(len(opened)), group), false)
	}
	aliases, reason := failedAliases(failed)
	if len(opened) == 0 {
		return m.setStatus(fmt.Sprintf("could not open %s in group %s%s", aliases, group, reason), true)
	}
	return m.setStatus(fmt.Sprintf("opened %s in group %s; could not open %s%s", countConnections(len(opened)), group, aliases, reason), true)
}

// failedAliases lists the aliases that failed to start. When they all failed
// for the same reason, reason is ": <error>"; otherwise each alias carries
// its own error in parentheses and reason is empty.
func failedAliases(failed []*startError) (aliases, reason string) {
	same := true
	for _, f := range failed {
		same = same && f.err.Error() == failed[0].err.Error()
	}
	parts := make([]string, len(failed))
	for i, f := range failed {
		parts[i] = f.alias
		if !same {
			parts[i] += " (" + f.err.Error() + ")"
		}
	}
	if same {
		reason = ": " + failed[0].err.Error()
	}
	return strings.Join(parts, ", "), reason
}

func countConnections(n int) string {
	if n == 1 {
		return "1 connection"
	}
	return fmt.Sprintf("%d connections", n)
}

// reconnect starts a new process in the current tab after its Session ended.
func (m *Model) reconnect() tea.Cmd {
	c := m.st.Active()
	if c == nil || !c.CurrentSession().Ended {
		return nil
	}
	if err := m.sessions.reconnect(c.CurrentSession()); err != nil {
		if errors.Is(err, ErrOrphaned) {
			return m.setStatus(c.Alias+" is missing from ssh_config", true)
		}
		return m.setStatus(err.Error(), true)
	}
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
		m.sessions.closeCurrent()
		m.scrolling = false
		if m.st.Active() == nil {
			m.focus = focusList
		}
		return nil
	}
	if sess.Ended {
		return doClose(m)
	}
	m.ask(fmt.Sprintf("Close %s %d? (y/n)", c.Alias, sess.N), doClose)
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
	m.ask(fmt.Sprintf("%d live %s will be closed. Quit? (y/n)", n, noun), func(*Model) tea.Cmd { return tea.Quit })
	return nil
}

// reload reloads the connection list from disk, puts the cursor on alias
// when it is set, and shows status.
func (m *Model) reload(alias, status string) tea.Cmd {
	snap, err := m.opts.Catalog.Snapshot()
	if err != nil {
		return m.setStatus("reload: "+err.Error(), true)
	}
	m.st.Reload(snap.Entries)
	m.st.SetSpaces(snap.Spaces, snap.ActiveSpace)
	m.st.SetGroups(snap.Groups)
	m.identity = snap.Identity
	if alias != "" {
		m.st.SetCursor(alias)
	}
	return m.setStatus(status, false)
}

// switchSpace makes Space number n (counting from 1) the active Space; see
// showSpace.
func (m *Model) switchSpace(n int) tea.Cmd {
	ref, ok := m.st.SpaceRef(n)
	if !ok {
		return m.setStatus(fmt.Sprintf("no space %d", n), true)
	}
	return m.showSpace(ref)
}

// showSpace makes the Space ref ("" for the default Space) the active Space
// and saves it. The list gets focus unless that Space has an Active
// connection. Sessions in every Space keep running.
func (m *Model) showSpace(ref string) tea.Cmd {
	m.st.SetSpace(ref)
	m.scrolling = false
	if m.st.Active() == nil {
		m.focus = focusList
	}
	if err := m.opts.Catalog.SetActiveSpace(ref); err != nil {
		return m.setStatus("save active space: "+err.Error(), true)
	}
	return nil
}

// showSpaceNote shows every Space's number and name under the Space bar,
// or on the status line while the list is hidden, until the next key or
// spaceNoteTimeout.
func (m *Model) showSpaceNote() tea.Cmd {
	m.spaceNote = true
	m.spaceNoteID++
	id := m.spaceNoteID
	return tea.Tick(spaceNoteTimeout, func(time.Time) tea.Msg { return hideSpaceNoteMsg{id} })
}

// setCollapsed collapses or expands group and saves the collapsed groups.
func (m *Model) setCollapsed(group string, collapsed bool) tea.Cmd {
	if m.st.Collapsed(group) == collapsed {
		return nil
	}
	m.st.SetCollapsed(group, collapsed)
	if err := m.opts.Catalog.SetCollapsed(m.st.CollapsedGroups()); err != nil {
		return m.setStatus("save collapsed groups: "+err.Error(), true)
	}
	return nil
}

// shiftGroup moves the Group whose header is under the cursor delta places
// up or down. It does nothing on a Connection row, on the ungrouped header,
// or when the Group is already first or last.
func (m *Model) shiftGroup(delta int) tea.Cmd {
	r, ok := m.st.CursorRow()
	if !ok || !r.Header() {
		return nil
	}
	pos := m.st.GroupPosition(r.Group)
	if pos == 0 {
		return nil
	}
	to := pos + delta
	if to < 1 || to > len(m.st.groups) {
		return nil
	}
	return m.moveGroup(r.Group, to)
}

// moveGroup saves group at position (counting from 1) and reloads the list
// in the new order.
func (m *Model) moveGroup(group string, position int) tea.Cmd {
	if err := m.opts.Catalog.MoveGroup(m.st.Space(), group, position); err != nil {
		return m.setStatus("move group "+group+": "+err.Error(), true)
	}
	return m.reload("", fmt.Sprintf("moved group %s to position %d", group, position))
}

// moveTarget is the Connection a move or an Action applies to: the Active connection
// while the terminal has focus, otherwise the one under the cursor.
func (m *Model) moveTarget() *Connection {
	if m.focus == focusPane {
		if c := m.st.Active(); c != nil {
			return c
		}
	}
	return m.st.Cursor()
}

func (m *Model) startMove() tea.Cmd {
	c := m.moveTarget()
	if c == nil {
		return m.setStatus("no connection to move", false)
	}
	m.openModal(&moveInput{alias: c.Alias, group: newLine(c.Group)})
	return nil
}

// moveTo saves alias's new group and moves it there in the list. group is a
// Group number (0 ungroups) or a name; an unknown name asks to create the
// Group first. A Connection moved into a collapsed group leaves the cursor
// on that group's header.
func (m *Model) moveTo(alias, group string) tea.Cmd {
	typed := strings.TrimSpace(group)
	group, ok := m.st.ResolveGroup(typed)
	if !ok {
		return m.setStatus("no group number "+typed, true)
	}
	c := m.st.Get(alias)
	if c == nil || c.Group == group {
		return nil
	}
	if group != "" && !m.st.HasGroup(group) {
		m.askCreateGroup(m.st.Space(), group, func(m *Model) tea.Cmd {
			if cmd := m.reload("", ""); m.statusErr {
				return cmd
			}
			return m.moveTo(alias, group)
		})
		return nil
	}
	from := c.Group
	if err := m.opts.Catalog.SetGroup(alias, group); err != nil {
		return m.setStatus("move "+alias+": "+err.Error(), true)
	}
	m.st.SetGroup(alias, group)
	if group == "" {
		return m.setStatus(fmt.Sprintf("removed %s from group %s", alias, from), false)
	}
	return m.setStatus(fmt.Sprintf("moved %s to group %s", alias, group), false)
}

func (m *Model) setStatus(s string, isErr bool) tea.Cmd {
	m.statusID++
	m.status, m.statusErr = s, isErr
	id := m.statusID
	return tea.Tick(statusTimeout, func(time.Time) tea.Msg { return clearStatusMsg{id} })
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.modal != nil {
		return nil
	}
	mm := msg.Mouse()
	if m.drag != nil {
		switch msg.(type) {
		case tea.MouseMotionMsg:
			m.dragOver(mm)
			return nil
		case tea.MouseReleaseMsg:
			return m.drop(mm)
		}
	}
	if mm.Y >= m.h-1 {
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
		row, ok := m.rowAt(mm.X, mm.Y)
		if !ok {
			return nil
		}
		if row.Header() {
			// Released where it was pressed, it toggles; see drop.
			m.st.SetCursorGroup(row.Group)
			m.focus = focusList
			m.drag = &drag{group: row.Group, fromY: mm.Y}
			return nil
		}
		c := row.Conn
		m.drag = &drag{alias: c.Alias, fromY: mm.Y}
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

// rowAt returns the list row at screen position x, y.
func (m *Model) rowAt(x, y int) (Row, bool) {
	if x < 0 || x >= m.listWidth() || y >= m.h-1 {
		return Row{}, false
	}
	rows, offset, top := m.listRows()
	i := y - top + offset
	if y < top || i < 0 || i >= len(rows) {
		return Row{}, false
	}
	return rows[i], true
}

// dragOver follows a held Connection. Once it leaves its own row the list
// also shows the ungrouped header, so it can be dropped out of its group.
func (m *Model) dragOver(mm tea.Mouse) {
	d := m.drag
	if !d.moving {
		if mm.Y == d.fromY || mm.Button != tea.MouseLeft {
			return
		}
		d.moving = true
		if d.group == "" {
			m.st.ShowUngrouped(true)
		}
	}
	row, ok := m.rowAt(mm.X, mm.Y)
	if d.group != "" {
		ok = ok && row.Header() && row.Group != d.group && m.st.GroupPosition(row.Group) > 0
	}
	d.target, d.ok = row.Group, ok
}

// drop moves the held Connection to the group of the row it is released on,
// or the held Group to the position of the Group header it is released on.
// Released off the list, or without having moved, nothing changes, except
// that a Group header pressed and released in place toggles.
func (m *Model) drop(mm tea.Mouse) tea.Cmd {
	d := m.drag
	m.drag = nil
	if d.group != "" {
		switch {
		case !d.moving:
			return m.setCollapsed(d.group, !m.st.Collapsed(d.group))
		case d.ok:
			return m.moveGroup(d.group, m.st.GroupPosition(d.target))
		}
		return m.setStatus("move cancelled", false)
	}
	if !d.moving {
		return nil
	}
	row, ok := m.rowAt(mm.X, mm.Y)
	m.st.ShowUngrouped(false)
	if !ok {
		return m.setStatus("move cancelled", false)
	}
	return m.moveTo(d.alias, row.Group)
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
	if m.spaceBar() != "" {
		top++
	}
	top += len(m.spaceNoteLines(m.listWidth()))
	if m.filtering || m.st.Filter() != "" {
		top++
	}
	height := m.h - 1 - top
	cur := m.st.CursorIndex(rows)
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
