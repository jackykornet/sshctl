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

// Options configures a Workspace.
type Options struct {
	// Catalog loads and saves Connections and the Workspace's saved state.
	// It is required.
	Catalog Catalog
	// Command returns the command line for a new Session of alias that runs
	// what l says: ssh or one of sshctl's own
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
	// ReadClipboard reads the system clipboard for a right-click paste. When
	// it is nil or returns no text, the Workspace asks the host terminal for
	// the clipboard with OSC 52 instead.
	ReadClipboard func() (string, error)
	// CommandNames are sshctl's built-in commands. An alias equal to one
	// is reached only with sshctl -c <alias> (ADR-0011), so the add/edit
	// form warns about it.
	CommandNames []string
}

type focus int

const (
	focusList focus = iota
	focusPane
)

type clearStatusMsg struct{ id int }

// hideSpaceNoteMsg hides the Space note shown with the same id.
type hideSpaceNoteMsg struct{ id int }

// Model is the Workspace's Bubble Tea model.
type Model struct {
	opts Options
	st   *State
	// identities holds the Snapshot's Selected identities, global and per
	// Space, for the status line's key (see selectedIdentities).
	identities Snapshot
	// features says which Experimental features are on, as of the last
	// Snapshot.
	features catalog.Experimental
	ev       *events
	sessions *sessions
	// tunnels are the Connections' Tunnels running in the background, from
	// every Space; they are not Sessions.
	tunnels *tunnels
	// tunnelCursor is the row of the Tunnels section the list cursor is
	// on, or -1 while it is on the connection list.
	tunnelCursor int
	w, h         int
	focus        focus
	prefix       bool // armed in any mode but modal; a modal keeps its own
	// mode is list, filter, scroll, drag, select or modal; it changes only
	// through setMode.
	mode     mode
	hideList bool
	// lastClick is the last left click in a Session pane, for double-clicks.
	lastClick paneClick
	// rightPaste is the right-click paste waiting for the clipboard, if any.
	rightPaste *pendingPaste
	pastes     int // right-click pastes started, for their ids
	status     string
	statusErr  bool
	statusID   int
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
	m := &Model{opts: opts, st: st, identities: selectedIdentities(snap), features: snap.Experimental, ev: newEvents()}
	m.sessions = &sessions{
		st:         st,
		ev:         m.ev,
		command:    opts.Command,
		scrollback: opts.ScrollbackLines,
		size:       m.paneSize,
		runners:    map[*Session]*runner{},
	}
	m.tunnels = &tunnels{ev: m.ev, command: opts.Command}
	m.tunnelCursor = -1
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

// closeAll ends every Session's process and stops every tunnel.
func (m *Model) closeAll() {
	m.sessions.closeAll()
	m.tunnels.stopAll()
}

func (m *Model) Init() tea.Cmd { return m.ev.listen() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Before msg acts on a scroll position, account for output that arrived.
	m.keepViews()
	cmd := m.update(msg)
	m.syncSize()
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	if cmd, ok := m.clipboardRead(msg); ok {
		return cmd
	}
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
		m.takeActivity()
		return m.ev.listen()
	case sessionEndedMsg:
		m.takeActivity() // its last output may not have been redrawn yet
		if m.sessions.ended(msg) && msg.sess.Action != "" && msg.code == 0 {
			// An Action may have rewritten the Connection's Host block.
			return tea.Batch(m.reload("", string(msg.sess.Action)+" finished; connection list refreshed"), m.ev.listen())
		}
		return m.ev.listen()
	case tunnelExitedMsg:
		if r := m.tunnels.exited(msg); r != nil {
			return tea.Batch(m.setStatus(r.alias+" tunnel failed: "+r.err, true), m.ev.listen())
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

// takeActivity marks the Sessions that wrote output since the last call, and
// clears the selection when its Session's output changed under it.
func (m *Model) takeActivity() {
	if sel := m.mode.sel; sel != nil {
		if r := m.sessions.runner(sel.sess); r != nil && r.activity.Load() {
			m.clearSelection()
		}
	}
	m.sessions.takeActivity()
}

// keepViews keeps every scrolled-back view on the history lines it shows as
// output arrives (see runner.keepView), and the selection on such a view
// with it.
func (m *Model) keepViews() {
	var r *runner
	before := 0
	sel := m.mode.sel
	if sel != nil {
		if r = m.sessions.runner(sel.sess); r != nil {
			before = r.scroll
		}
	}
	m.sessions.keepViews()
	if r != nil {
		sel.scroll += r.scroll - before
	}
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
	case m.mode.modal != nil:
		m.mode.modal.paste(text)
	case m.filtering():
		f := &m.base().filter
		f.paste(text)
		m.st.SetFilter(f.String())
	case r != nil && m.focus == focusPane:
		r.term.Paste(text)
	}
}

// key routes a key press: to the modal when one is open, then through the
// bindings for the context the Workspace is in (see bindings).
func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	m.spaceNote = false
	m.clearSelection()
	if md := m.mode.modal; md != nil {
		return md.key(m, msg)
	}
	if m.prefix {
		m.prefix = false
		return m.prefixCommand(msg)
	}
	if msg.String() == m.opts.PrefixKey {
		m.prefix = true
		return nil
	}
	if m.filtering() {
		m.filterKey(msg)
		return nil
	}
	if m.focus == focusPane {
		return m.paneKey(msg)
	}
	return m.listKey(msg)
}

// paneKey handles a key while the terminal pane has focus: scroll mode keys,
// the ended-Session actions, or forwarding to the live Session.
func (m *Model) paneKey(msg tea.KeyPressMsg) tea.Cmd {
	s, r := m.currentRunner()
	if s == nil {
		return nil
	}
	if m.scrolling() && r != nil {
		if bk, ok := findKey(m.scrollKeys(), msg.String()); ok {
			bk.run(m, msg)
		}
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

// scrollKeys are scroll mode's keys, which act on the current tab's runner.
func (m *Model) scrollKeys() []boundKey {
	_, h := m.paneSize()
	by := func(n func(r *runner) int) action {
		return func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
			if _, r := m.currentRunner(); r != nil {
				m.scrollBy(r, n(r))
			}
			return nil
		}
	}
	lines := func(n int) action { return by(func(*runner) int { return n }) }
	top := by(func(r *runner) int { return r.term.ScrollbackLen() })
	exit := func(m *Model, _ tea.KeyPressMsg) tea.Cmd {
		if _, r := m.currentRunner(); r != nil {
			r.scroll = 0
		}
		m.stopScroll()
		return nil
	}
	return []boundKey{
		on("up", lines(1)).as("scroll"), on("k", lines(1)),
		on("down", lines(-1)).as("scroll"), on("j", lines(-1)),
		on("pgup", lines(h)).as("scroll"),
		on("pgdown", lines(-h)).as("scroll"), on("ctrl+f", lines(-h)), on("space", lines(-h)),
		on("g", top).as("top"), on("home", top),
		on("G", exit).as("bottom"), on("end", exit),
		on("q", exit).as("exit"), on("esc", exit),
	}
}

// scrollHints is scroll mode's hint line.
func (m *Model) scrollHints() hintLine {
	return hintsFor(m.scrollKeys(), "up", "down", "pgup", "pgdown", "g", "G", "q")
}

func (m *Model) scrollBy(r *runner, n int) {
	r.keepView() // count from the history as it is now
	r.scroll = max(0, min(r.scroll+n, r.term.ScrollbackLen()))
	switch {
	case r.scroll == 0:
		m.stopScroll()
	case !m.filtering(): // the wheel does not end typing the filter
		m.setBase(mode{kind: modeScroll})
	}
}

// startFilter starts typing the filter, resuming the current one with the
// cursor at its end.
func (m *Model) startFilter() {
	m.setBase(mode{kind: modeFilter, filter: newLine(m.st.Filter())})
}

func (m *Model) filterKey(msg tea.KeyPressMsg) {
	switch f := &m.base().filter; msg.String() {
	case "esc":
		m.setBase(mode{kind: modeList})
		m.st.SetFilter("")
	case "enter":
		m.setBase(mode{kind: modeList})
	default:
		if f.key(msg) {
			m.st.SetFilter(f.String())
		}
	}
}

func (m *Model) prefixCommand(msg tea.KeyPressMsg) tea.Cmd {
	if cmd, ok := m.runBinding(inPrefix, msg); ok {
		return cmd
	}
	p := keyHint(m.opts.PrefixKey)
	return m.setStatus(fmt.Sprintf("unknown command %s %s · %s", p, msg.String(), m.hint(inPrefix, "?")), true)
}

func (m *Model) selectTab(delta int) {
	m.st.SelectTab(delta)
	m.stopScroll()
}

func (m *Model) enter() tea.Cmd {
	if r, ok := m.st.CursorRow(); ok && r.Header() {
		return m.setCollapsed(r.Group, !m.st.Collapsed(r.Group))
	}
	c := m.st.Cursor()
	if c == nil {
		return nil
	}
	saved := m.showSpaceOf(c)
	if c.Open() {
		m.st.Activate(c.Alias)
		m.focus = focusPane
		return saved
	}
	return tea.Batch(saved, m.open(c.Alias, Launch{}))
}

// showSpaceOf makes c's Space the active Space when the filter found c in
// another Space, so whatever acts on c acts in its own Space: opening it,
// a new tab or Action, its Group, the form. The cursor stays on c.
func (m *Model) showSpaceOf(c *Connection) tea.Cmd {
	if c.Space == m.st.Space() {
		return nil
	}
	cmd := m.showSpace(c.Space)
	m.st.SetCursor(c.Alias)
	return cmd
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
	m.focus = focusPane
	m.stopScroll()
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
		return m.setStatus(fmt.Sprintf("opened %s in group %s", plural(len(opened), "connection"), group), false)
	}
	aliases, reason := failedAliases(failed)
	if len(opened) == 0 {
		return m.setStatus(fmt.Sprintf("could not open %s in group %s%s", aliases, group, reason), true)
	}
	return m.setStatus(fmt.Sprintf("opened %s in group %s; could not open %s%s", plural(len(opened), "connection"), group, aliases, reason), true)
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

// plural is n of noun, as "1 tunnel" or "2 tunnels".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
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
	m.stopScroll()
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
		m.stopScroll()
		if m.st.Active() == nil {
			m.focus = focusList
		}
		return nil
	}
	if sess.Ended {
		return doClose(m)
	}
	m.ask(fmt.Sprintf("Close %s %d?", c.Alias, sess.N), doClose)
	return nil
}

// quit quits, first asking while Sessions are live or tunnels run.
// Confirming stops every tunnel before the Workspace exits.
func (m *Model) quit() tea.Cmd {
	n, k := m.st.LiveSessions(), m.tunnels.count()
	sessions, tunnels := plural(n, "live session"), plural(k, "tunnel")
	var question string
	switch {
	case n == 0 && k == 0:
		return tea.Quit
	case k == 0:
		question = fmt.Sprintf("%s will be closed. Quit?", sessions)
	case n == 0:
		question = fmt.Sprintf("%s running, quit anyway?", tunnels)
	default:
		question = fmt.Sprintf("%s and %s running, quit anyway?", sessions, tunnels)
	}
	m.ask(question, func(m *Model) tea.Cmd {
		m.tunnels.stopAll()
		return tea.Quit
	})
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
	m.identities = selectedIdentities(snap)
	m.features = snap.Experimental
	if alias != "" {
		m.st.SetCursor(alias)
	}
	return m.setStatus(status, false)
}

// selectedIdentities is the part of snap that says which key each
// Space's Connections use.
func selectedIdentities(snap Snapshot) Snapshot {
	return Snapshot{Identity: snap.Identity, SpaceIdentities: snap.SpaceIdentities}
}

// switchSpace makes the Space with Space number n the active Space; see
// showSpace. A free number switches nothing.
func (m *Model) switchSpace(n int) tea.Cmd {
	ref, ok := m.st.SpaceRef(n)
	if !ok {
		return m.setStatus(fmt.Sprintf("no Space %d", n), true)
	}
	return m.showSpace(ref)
}

// showSpace makes the Space ref ("" for the default Space) the active Space
// and saves it. The list gets focus unless that Space has an Active
// connection. Sessions in every Space keep running.
func (m *Model) showSpace(ref string) tea.Cmd {
	m.st.SetSpace(ref)
	m.stopScroll()
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

func keyHint(k string) string {
	if rest, ok := strings.CutPrefix(k, "ctrl+"); ok && len(rest) == 1 {
		return "^" + strings.ToUpper(rest)
	}
	return k
}
