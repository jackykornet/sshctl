// Package workspace implements the Workspace: the full-screen view shown when
// sshctl runs without arguments.
package workspace

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
)

// Entry is one Connection as loaded from ssh_config and sshctl metadata.
type Entry = catalog.Entry

// Session is one tab of a Connection.
type Session struct {
	// N is the tab number, unique per Connection for the lifetime of the Workspace.
	N        int
	Ended    bool
	ExitCode int
	// Unseen is true when the Session had activity (output or a bell) while
	// it was not the shown tab. Showing the tab clears it.
	Unseen bool
	// Launch is what the Session's process runs.
	Launch
}

// A Launch is what a Session's process runs for its Connection: ssh to it
// (a login shell, or claude), or one of sshctl's own Actions on it.
type Launch struct {
	// Claude runs claude on the remote host instead of a login shell.
	Claude bool
	// Action, when set, runs sshctl's own command of that name on the
	// Connection ("sshctl bootstrap <alias>") instead of ssh. Its tab is
	// labelled with the Action, and when it exits with status 0 the
	// connection list is reloaded, since an Action may change Host blocks.
	Action Action
}

// An Action is an sshctl command a Session can run on its Connection.
type Action string

// ActionBootstrap runs Bootstrap ("sshctl bootstrap <alias>").
const ActionBootstrap Action = "bootstrap"

// Connection is a Connection together with its Sessions in this Workspace.
type Connection struct {
	Entry
	Sessions []*Session
	// Current is the index of the Session shown when this Connection is active.
	Current int
	nextN   int
}

// Open reports whether the Connection has at least one Session, live or ended.
func (c *Connection) Open() bool { return len(c.Sessions) > 0 }

// AllEnded reports whether the Connection is open but none of its Sessions are live.
func (c *Connection) AllEnded() bool {
	if !c.Open() {
		return false
	}
	for _, s := range c.Sessions {
		if !s.Ended {
			return false
		}
	}
	return true
}

// Unseen reports whether any of the Connection's Sessions has unseen activity.
func (c *Connection) Unseen() bool {
	for _, s := range c.Sessions {
		if s.Unseen {
			return true
		}
	}
	return false
}

// LiveSessions counts Sessions whose process is still running.
func (c *Connection) LiveSessions() int {
	n := 0
	for _, s := range c.Sessions {
		if !s.Ended {
			n++
		}
	}
	return n
}

// CurrentSession returns the Session shown for this Connection, or nil.
func (c *Connection) CurrentSession() *Session {
	if !c.Open() {
		return nil
	}
	return c.Sessions[c.Current]
}

// ErrOrphaned is returned when opening a Connection whose Host alias is gone.
var ErrOrphaned = errors.New("missing from ssh_config")

// Row is one line of the connection list: a group header or a Connection.
type Row struct {
	// Group is the header's group, or the Connection's group. "" is the
	// ungrouped section.
	Group string
	// Conn is nil for a group header.
	Conn *Connection
}

// Header reports whether the row is a group header.
func (r Row) Header() bool { return r.Conn == nil }

// rowKey identifies a row across rebuilds of the list: a header by its group,
// a Connection by its alias.
type rowKey struct {
	header bool
	name   string
}

func keyOf(r Row) rowKey {
	if r.Header() {
		return rowKey{true, r.Group}
	}
	return rowKey{false, r.Conn.Alias}
}

// State holds the Workspace's connection list and Sessions, independent of rendering.
type State struct {
	conns     map[string]*Connection
	loaded    map[string]bool // aliases returned by the last Reload
	groups    []string        // the stored Groups in the user's order
	collapsed map[string]bool
	// showUngrouped lists the ungrouped header even when it has no members,
	// so a Connection can be dragged out of its group.
	showUngrouped bool
	active        string
	cursor        rowKey
	filter        string
}

// NewState builds a State from loaded entries.
func NewState(entries []Entry) *State {
	s := &State{conns: map[string]*Connection{}, collapsed: map[string]bool{}}
	s.Reload(entries)
	return s
}

// Reload replaces the loaded entries while keeping every open Connection and its Sessions.
// An open Connection that is no longer loaded becomes orphaned.
func (s *State) Reload(entries []Entry) {
	loaded := map[string]bool{}
	s.loaded = loaded
	for _, e := range entries {
		loaded[e.Alias] = true
		if c, ok := s.conns[e.Alias]; ok {
			c.Entry = e
		} else {
			s.conns[e.Alias] = &Connection{Entry: e}
		}
	}
	for alias, c := range s.conns {
		if loaded[alias] {
			continue
		}
		if c.Open() {
			c.Orphaned = true
		} else {
			delete(s.conns, alias)
		}
	}
	s.fixCursor()
}

// SetGroups sets the stored Groups, in the user's order.
func (s *State) SetGroups(groups []string) {
	s.groups = slices.Clone(groups)
	s.fixCursor()
}

// Groups returns every Group in list order: the stored Groups in the user's
// order, then any Group only a Connection names, alphabetically.
func (s *State) Groups() []string {
	out := slices.Clone(s.groups)
	var extra []string
	for _, c := range s.conns {
		if c.Group != "" && !slices.Contains(out, c.Group) && !slices.Contains(extra, c.Group) {
			extra = append(extra, c.Group)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// GroupPosition is group's position among the stored Groups, counting from
// 1, or 0 when group is not a stored Group.
func (s *State) GroupPosition(group string) int {
	return slices.Index(s.groups, group) + 1
}

// HasGroup reports whether group is a Group the list shows.
func (s *State) HasGroup(group string) bool {
	return group != "" && slices.Contains(s.Groups(), group)
}

// Rows returns the connection list in display order. Without any groups it
// is every Connection alphabetically. With groups, each group (in Groups
// order, empty ones included, the ungrouped section last) is a header
// followed by its members alphabetically, unless the group is collapsed.
// Opening a Connection never moves it. The filter hides Connections that
// are not open, and groups left with no rows; while filtering, every group
// is shown expanded.
func (s *State) Rows() []Row {
	conns := s.ordered()
	if !s.grouped() {
		var rows []Row
		for _, c := range conns {
			if c.Open() || s.matches(c) {
				rows = append(rows, Row{Conn: c})
			}
		}
		return rows
	}
	filtering := s.filter != ""
	var rows []Row
	section := func(group string) {
		var members []Row
		for _, c := range conns {
			if c.Group == group && (c.Open() || s.matches(c)) {
				members = append(members, Row{Group: group, Conn: c})
			}
		}
		if filtering && len(members) == 0 {
			return
		}
		rows = append(rows, Row{Group: group})
		if filtering || !s.collapsed[group] {
			rows = append(rows, members...)
		}
	}
	for _, g := range s.Groups() {
		section(g)
	}
	hasUngrouped := slices.ContainsFunc(conns, func(c *Connection) bool { return c.Group == "" })
	if hasUngrouped || (s.showUngrouped && !filtering) {
		section("")
	}
	return rows
}

// ordered returns every Connection sorted by group (in Groups order,
// ungrouped last), then by alias.
func (s *State) ordered() []*Connection {
	rank := map[string]int{"": len(s.groups) + len(s.conns)}
	for i, g := range s.Groups() {
		rank[g] = i
	}
	out := make([]*Connection, 0, len(s.conns))
	for _, c := range s.conns {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Group != b.Group {
			return rank[a.Group] < rank[b.Group]
		}
		return a.Alias < b.Alias
	})
	return out
}

// grouped reports whether there is any Group, which is when the list shows
// group headers.
func (s *State) grouped() bool { return len(s.Groups()) > 0 }

// Members returns every Connection in group alphabetically, ignoring the
// filter and whether the group is collapsed.
func (s *State) Members(group string) []*Connection {
	var out []*Connection
	for _, c := range s.ordered() {
		if c.Group == group {
			out = append(out, c)
		}
	}
	return out
}

// GroupToOpen returns the Connections that opening group would open: its
// closed, non-orphaned members in list order, ignoring the filter.
func (s *State) GroupToOpen(group string) []*Connection {
	if group == "" {
		return nil
	}
	var out []*Connection
	for _, c := range s.Members(group) {
		if !c.Open() && !c.Orphaned {
			out = append(out, c)
		}
	}
	return out
}

// Collapsed reports whether group's members are hidden.
func (s *State) Collapsed(group string) bool { return s.collapsed[group] }

// SetCollapsed hides or shows group's members. A cursor on a hidden member
// moves to the group's header.
func (s *State) SetCollapsed(group string, collapsed bool) {
	if collapsed {
		s.collapsed[group] = true
	} else {
		delete(s.collapsed, group)
	}
	s.fixCursor()
}

// CollapsedGroups returns the collapsed groups alphabetically.
func (s *State) CollapsedGroups() []string {
	out := make([]string, 0, len(s.collapsed))
	for g := range s.collapsed {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}

// SetGroup moves alias into group; "" takes it out of its group.
func (s *State) SetGroup(alias, group string) {
	if c := s.conns[alias]; c != nil {
		c.Group = group
		s.fixCursor()
	}
}

// ShowUngrouped lists the ungrouped header even when it has no members.
func (s *State) ShowUngrouped(show bool) {
	s.showUngrouped = show
	s.fixCursor()
}

func (s *State) matches(c *Connection) bool {
	if s.filter == "" {
		return true
	}
	f := strings.ToLower(s.filter)
	for _, field := range []string{c.Alias, c.Description, c.Group} {
		if strings.Contains(strings.ToLower(field), f) {
			return true
		}
	}
	return false
}

// Get returns the Connection for alias, or nil.
func (s *State) Get(alias string) *Connection { return s.conns[alias] }

// Active returns the Active connection, or nil when none is selected.
func (s *State) Active() *Connection { return s.conns[s.active] }

// Cursor returns the Connection under the list cursor, or nil when the
// cursor is on a group header or the list is empty.
func (s *State) Cursor() *Connection {
	if s.cursor.header {
		return nil
	}
	return s.conns[s.cursor.name]
}

// CursorRow returns the row under the list cursor; ok is false when the list is empty.
func (s *State) CursorRow() (r Row, ok bool) {
	if s.cursor.header {
		return Row{Group: s.cursor.name}, true
	}
	if c := s.conns[s.cursor.name]; c != nil {
		return Row{Group: c.Group, Conn: c}, true
	}
	return Row{}, false
}

// CursorIndex returns the cursor's index in rows, or -1.
func (s *State) CursorIndex(rows []Row) int {
	for i, r := range rows {
		if keyOf(r) == s.cursor {
			return i
		}
	}
	return -1
}

// Filter returns the current list filter.
func (s *State) Filter() string { return s.filter }

// SetFilter changes the list filter and keeps the cursor on a visible row.
func (s *State) SetFilter(f string) {
	s.filter = f
	s.fixCursor()
}

// MoveCursor moves the list cursor by delta rows, stopping at either end.
func (s *State) MoveCursor(delta int) {
	rows := s.Rows()
	if len(rows) == 0 {
		return
	}
	i := s.CursorIndex(rows) + delta
	i = max(0, min(i, len(rows)-1))
	s.cursor = keyOf(rows[i])
}

// SetCursor puts the list cursor on alias, or on its group's header when the
// group is collapsed.
func (s *State) SetCursor(alias string) {
	if _, ok := s.conns[alias]; ok {
		s.cursor = rowKey{false, alias}
		s.fixCursor()
	}
}

// SetCursorGroup puts the list cursor on group's header.
func (s *State) SetCursorGroup(group string) {
	s.cursor = rowKey{true, group}
	s.fixCursor()
}

// Activate makes an open Connection the Active connection.
func (s *State) Activate(alias string) error {
	c := s.conns[alias]
	if c == nil {
		return fmt.Errorf("unknown connection %q", alias)
	}
	if !c.Open() {
		return fmt.Errorf("%s is not open", alias)
	}
	s.active = alias
	s.SetCursor(alias)
	s.clearShownActivity()
	return nil
}

// CycleActive makes the next (delta > 0) or previous open Connection active,
// in list order, including Connections in collapsed groups.
func (s *State) CycleActive(delta int) {
	var open []*Connection
	for _, c := range s.ordered() {
		if c.Open() {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return
	}
	i := slices.IndexFunc(open, func(c *Connection) bool { return c.Alias == s.active })
	if i < 0 {
		i = 0
	} else {
		i = (i + delta + len(open)) % len(open)
	}
	s.active = open[i].Alias
	s.SetCursor(open[i].Alias)
	s.clearShownActivity()
}

// OpenSession adds a new Session to alias, makes it the current tab and makes
// the Connection active.
func (s *State) OpenSession(alias string) (*Session, error) {
	if err := s.checkOpen(alias); err != nil {
		return nil, err
	}
	sess := &Session{}
	s.addSession(alias, sess)
	return sess, nil
}

// checkOpen reports why a Session cannot be opened for alias, or nil.
func (s *State) checkOpen(alias string) error {
	c := s.conns[alias]
	if c == nil {
		return fmt.Errorf("unknown connection %q", alias)
	}
	if c.Orphaned {
		return fmt.Errorf("%s: %w", alias, ErrOrphaned)
	}
	return nil
}

// addSession numbers sess, adds it to alias as the current tab and makes the
// Connection active. checkOpen must have accepted alias.
func (s *State) addSession(alias string, sess *Session) {
	c := s.conns[alias]
	c.nextN++
	sess.N = c.nextN
	c.Sessions = append(c.Sessions, sess)
	c.Current = len(c.Sessions) - 1
	s.active = alias
	s.SetCursor(alias)
}

// SelectTab moves the Active connection's current tab by delta, stopping at either end.
func (s *State) SelectTab(delta int) {
	c := s.Active()
	if c == nil || !c.Open() {
		return
	}
	c.Current = max(0, min(c.Current+delta, len(c.Sessions)-1))
	s.clearShownActivity()
}

// SelectTabIndex makes tab i of the Active connection current.
func (s *State) SelectTabIndex(i int) {
	if c := s.Active(); c != nil && i >= 0 && i < len(c.Sessions) {
		c.Current = i
		s.clearShownActivity()
	}
}

// connectionOf returns the Connection sess is a tab of, or nil once it is closed.
func (s *State) connectionOf(sess *Session) *Connection {
	for _, c := range s.conns {
		if slices.Contains(c.Sessions, sess) {
			return c
		}
	}
	return nil
}

// CloseCurrent closes the Active connection's current tab. When it was the
// last tab, the Connection stops being open and no Connection is active.
func (s *State) CloseCurrent() *Session {
	c := s.Active()
	if c == nil || !c.Open() {
		return nil
	}
	closed := c.Sessions[c.Current]
	c.Sessions = append(c.Sessions[:c.Current], c.Sessions[c.Current+1:]...)
	if c.Current >= len(c.Sessions) {
		c.Current = max(len(c.Sessions)-1, 0)
	}
	if !c.Open() {
		s.active = ""
		if !s.loaded[c.Alias] {
			// It was only listed because it was open; nothing else keeps it.
			delete(s.conns, c.Alias)
			s.fixCursor()
		}
	}
	s.clearShownActivity()
	return closed
}

// MarkActivity records that sess had output or a bell. It has unseen
// activity unless it is the shown tab of the Active connection.
func (s *State) MarkActivity(sess *Session) {
	if c := s.Active(); c != nil && c.CurrentSession() == sess {
		return
	}
	sess.Unseen = true
}

// clearShownActivity clears the activity of the Active connection's shown tab.
func (s *State) clearShownActivity() {
	if c := s.Active(); c != nil && c.Open() {
		c.CurrentSession().Unseen = false
	}
}

// LiveSessions counts every live Session in the Workspace.
func (s *State) LiveSessions() int {
	n := 0
	for _, c := range s.conns {
		n += c.LiveSessions()
	}
	return n
}

// fixCursor keeps the cursor on a listed row: a hidden Connection's cursor
// moves to its group's header, anything else to the first row.
func (s *State) fixCursor() {
	rows := s.Rows()
	if s.CursorIndex(rows) >= 0 {
		return
	}
	if c := s.conns[s.cursor.name]; c != nil && !s.cursor.header {
		head := rowKey{true, c.Group}
		for _, r := range rows {
			if keyOf(r) == head {
				s.cursor = head
				return
			}
		}
	}
	s.cursor = rowKey{}
	if len(rows) > 0 {
		s.cursor = keyOf(rows[0])
	}
}
