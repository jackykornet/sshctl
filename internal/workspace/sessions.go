package workspace

import (
	"errors"
	"fmt"
)

// sessions owns every Session's lifecycle: its record in State (tab number,
// live or ended, exit code), the runner behind it, and through the runner its
// scroll position. The Model asks it for one intent at a time and never sets
// a Session's lifecycle fields itself.
type sessions struct {
	st *State
	ev *events
	// command returns the argv for a Session; nil starts no process (tests).
	command    func(alias string, l Launch) ([]string, error)
	scrollback int
	// size is the pane size a new process starts at.
	size    func() (w, h int)
	runners map[*Session]*runner
}

// open starts a new Session (tab) for alias running what l says, and makes
// it the Active connection's current tab. When the process cannot start,
// State is left as it was and the error is a *startError.
func (s *sessions) open(alias string, l Launch) (*Session, error) {
	if err := s.st.checkOpen(alias); err != nil {
		return nil, err
	}
	sess := &Session{Launch: l}
	if err := s.start(alias, sess); err != nil {
		return nil, &startError{alias, err}
	}
	s.st.addSession(alias, sess)
	return sess, nil
}

// ended records that a runner's process exited. It ends the Session only
// when r is still its runner, so a runner replaced by reconnect or released
// by close is ignored, and reports whether it ended the Session.
func (s *sessions) ended(msg sessionEndedMsg) bool {
	if s.runners[msg.sess] != msg.r {
		return false
	}
	msg.sess.Ended, msg.sess.ExitCode = true, msg.code
	return true
}

// reconnect starts a new process in sess's tab after it ended. It refuses a
// live Session and an orphaned Connection; when the process cannot start the
// Session stays ended and the error is a *startError.
func (s *sessions) reconnect(sess *Session) error {
	c := s.st.connectionOf(sess)
	switch {
	case c == nil:
		return errors.New("session is closed")
	case !sess.Ended:
		return errors.New("session is still running")
	case c.Orphaned:
		return fmt.Errorf("%s: %w", c.Alias, ErrOrphaned)
	}
	if err := s.start(c.Alias, sess); err != nil {
		return &startError{c.Alias, err}
	}
	sess.Ended, sess.ExitCode = false, 0
	return nil
}

// closeCurrent closes the Active connection's current tab and ends its
// process. Closing a Connection's last tab stops it being an Open connection.
func (s *sessions) closeCurrent() *Session {
	sess := s.st.CloseCurrent()
	if r := s.runners[sess]; r != nil {
		r.close()
		delete(s.runners, sess)
	}
	return sess
}

// start runs sess's process, replacing any previous one.
func (s *sessions) start(alias string, sess *Session) error {
	if s.command == nil {
		return nil
	}
	argv, err := s.command(alias, sess.Launch)
	if err != nil {
		return err
	}
	w, h := s.size()
	r, err := startRunner(argv, w, h, s.scrollback, sess, s.ev)
	if err != nil {
		return err
	}
	if old := s.runners[sess]; old != nil {
		old.close()
	}
	s.runners[sess] = r
	return nil
}

// runner returns the process behind sess, or nil.
func (s *sessions) runner(sess *Session) *runner { return s.runners[sess] }

// resize resizes every Session's terminal to w×h.
func (s *sessions) resize(w, h int) {
	for _, r := range s.runners {
		r.resize(w, h)
	}
}

// takeActivity marks every Session whose process wrote output since the last call.
func (s *sessions) takeActivity() {
	for sess, r := range s.runners {
		if r.activity.Swap(false) {
			s.st.MarkActivity(sess)
		}
	}
}

// closeAll ends every Session's process.
func (s *sessions) closeAll() {
	for sess, r := range s.runners {
		r.close()
		delete(s.runners, sess)
	}
}
