// Package workspace implements the Workspace: the full-screen view shown when
// sshctl runs without arguments.
package workspace

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Entry is one Connection as loaded from ssh_config and sshctl metadata.
type Entry struct {
	Alias       string
	Description string
	Group       string
	// Orphaned is true when sshctl metadata exists but the Host alias is gone.
	Orphaned bool
}

// Session is one tab of a Connection.
type Session struct {
	// N is the tab number, unique per Connection for the lifetime of the Workspace.
	N        int
	Ended    bool
	ExitCode int
	// Unseen is true when the Session had activity (output or a bell) while
	// it was not the shown tab. Showing the tab clears it.
	Unseen bool
}

// Connection is a Connection together with its Sessions in this Workspace.
type Connection struct {
	Entry
	Sessions []*Session
	// Current is the index of the Session shown when this Connection is active.
	Current   int
	nextN     int
	openedSeq int
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

// State holds the Workspace's connection list and Sessions, independent of rendering.
type State struct {
	conns  map[string]*Connection
	loaded map[string]bool // aliases returned by the last Reload
	seq    int
	active string
	cursor string
	filter string
}

// NewState builds a State from loaded entries.
func NewState(entries []Entry) *State {
	s := &State{conns: map[string]*Connection{}}
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

// Rows returns the connection list in display order: open Connections in the
// order they were opened, then the rest alphabetically. The filter hides only
// Connections that are not open.
func (s *State) Rows() []*Connection {
	var open, closed []*Connection
	for _, c := range s.conns {
		switch {
		case c.Open():
			open = append(open, c)
		case s.matches(c):
			closed = append(closed, c)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i].openedSeq < open[j].openedSeq })
	sort.Slice(closed, func(i, j int) bool { return closed[i].Alias < closed[j].Alias })
	return append(open, closed...)
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

// Cursor returns the Connection under the list cursor, or nil.
func (s *State) Cursor() *Connection { return s.conns[s.cursor] }

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
	i := s.index(rows, s.cursor) + delta
	i = max(0, min(i, len(rows)-1))
	s.cursor = rows[i].Alias
}

// SetCursor puts the list cursor on alias.
func (s *State) SetCursor(alias string) {
	if _, ok := s.conns[alias]; ok {
		s.cursor = alias
	}
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
	s.active, s.cursor = alias, alias
	s.shown()
	return nil
}

// CycleActive makes the next (delta > 0) or previous open Connection active.
func (s *State) CycleActive(delta int) {
	var open []*Connection
	for _, c := range s.Rows() {
		if c.Open() {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return
	}
	i := s.index(open, s.active)
	if i < 0 {
		i = 0
	} else {
		i = (i + delta + len(open)) % len(open)
	}
	s.active, s.cursor = open[i].Alias, open[i].Alias
	s.shown()
}

// OpenSession adds a new Session to alias, makes it the current tab and makes
// the Connection active.
func (s *State) OpenSession(alias string) (*Session, error) {
	c := s.conns[alias]
	if c == nil {
		return nil, fmt.Errorf("unknown connection %q", alias)
	}
	if c.Orphaned {
		return nil, fmt.Errorf("%s: %w", alias, ErrOrphaned)
	}
	if !c.Open() {
		s.seq++
		c.openedSeq = s.seq
	}
	c.nextN++
	sess := &Session{N: c.nextN}
	c.Sessions = append(c.Sessions, sess)
	c.Current = len(c.Sessions) - 1
	s.active, s.cursor = alias, alias
	return sess, nil
}

// SelectTab moves the Active connection's current tab by delta, stopping at either end.
func (s *State) SelectTab(delta int) {
	c := s.Active()
	if c == nil || !c.Open() {
		return
	}
	c.Current = max(0, min(c.Current+delta, len(c.Sessions)-1))
	s.shown()
}

// SelectTabIndex makes tab i of the Active connection current.
func (s *State) SelectTabIndex(i int) {
	if c := s.Active(); c != nil && i >= 0 && i < len(c.Sessions) {
		c.Current = i
		s.shown()
	}
}

// EndSession marks a Session as ended with the given exit code.
func (s *State) EndSession(alias string, n, code int) {
	c := s.conns[alias]
	if c == nil {
		return
	}
	for _, sess := range c.Sessions {
		if sess.N == n {
			sess.Ended, sess.ExitCode = true, code
		}
	}
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
		c.openedSeq = 0
		s.active = ""
		if !s.loaded[c.Alias] {
			// It was only listed because it was open; nothing else keeps it.
			delete(s.conns, c.Alias)
			s.fixCursor()
		}
	}
	s.shown()
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

// shown clears the activity of the Active connection's shown tab.
func (s *State) shown() {
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

func (s *State) fixCursor() {
	rows := s.Rows()
	if s.index(rows, s.cursor) >= 0 {
		return
	}
	s.cursor = ""
	if len(rows) > 0 {
		s.cursor = rows[0].Alias
	}
}

func (s *State) index(rows []*Connection, alias string) int {
	for i, c := range rows {
		if c.Alias == alias {
			return i
		}
	}
	return -1
}
