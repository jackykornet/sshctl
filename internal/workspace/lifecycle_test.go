package workspace

import (
	"errors"
	"testing"
	"time"
)

// Tests for the Session lifecycle module (m.sessions): open, end, reconnect,
// close and stale runner events.

func TestFailedStartLeavesNoTabAndKeepsActiveConnection(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}, {Alias: "beta"}}, "beta")
	if _, err := m.sessions.open("alpha", Launch{}); err != nil {
		t.Fatal(err)
	}
	_, err := m.sessions.open("beta", Launch{})
	var se *startError
	if !errors.As(err, &se) || se.alias != "beta" {
		t.Fatalf("err = %v, want a startError for beta", err)
	}
	if m.st.Get("beta").Open() {
		t.Fatal("a Session that failed to start must leave no tab behind")
	}
	if a := m.st.Active(); a == nil || a.Alias != "alpha" {
		t.Fatalf("active = %v, want alpha unchanged", a)
	}
	if got := r.started(); len(got) != 2 {
		t.Fatalf("Command calls = %v", got)
	}
}

func TestEndedProcessEndsSessionWithExitCode(t *testing.T) {
	m, _ := newSessionModel(t, script{sh: `exit 3`, cmd: `exit 3`})
	sess, err := m.sessions.open("alpha", Launch{})
	if err != nil {
		t.Fatal(err)
	}
	pump(t, m, func() bool { return sess.Ended })
	if sess.ExitCode != 3 || !m.st.Get("alpha").Open() {
		t.Fatalf("exit = %d open = %v; an ended tab keeps its Connection open", sess.ExitCode, m.st.Get("alpha").Open())
	}
}

func TestReconnectRestartsEndedSessionAndIgnoresOldRunner(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	sess, err := m.sessions.open("alpha", Launch{Action: ActionBootstrap})
	if err != nil {
		t.Fatal(err)
	}
	pump(t, m, func() bool { return sess.Ended })
	old := m.sessions.runner(sess)
	if err := m.sessions.reconnect(sess); err != nil {
		t.Fatal(err)
	}
	if sess.Ended || sess.ExitCode != 0 || m.sessions.runner(sess) == old {
		t.Fatal("reconnect should start a new live process in the same tab")
	}
	m.Update(sessionEndedMsg{sess: sess, r: old, code: 7})
	if sess.Ended {
		t.Fatal("an ended message from the replaced runner must be ignored")
	}
	if want := (commandCall{"alpha", Launch{Action: ActionBootstrap}}); len(r.calls) != 2 || r.calls[1] != want {
		t.Fatalf("Command calls = %v, want a second %v", r.calls, want)
	}
}

func TestReconnectRefusesLiveOrOrphanedSession(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "alpha"}})
	sess, err := m.sessions.open("alpha", Launch{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.sessions.reconnect(sess); err == nil || len(r.calls) != 1 {
		t.Fatalf("reconnecting a live Session: err = %v calls = %v", err, r.calls)
	}
	pump(t, m, func() bool { return sess.Ended })
	m.st.Reload(nil) // alpha's Host is gone; its open tab keeps it listed, orphaned
	if err := m.sessions.reconnect(sess); !errors.Is(err, ErrOrphaned) || !sess.Ended || len(r.calls) != 1 {
		t.Fatalf("reconnecting an orphaned Connection: err = %v ended = %v calls = %v", err, sess.Ended, r.calls)
	}
}

func TestClosingLastTabClosesConnectionAndRunner(t *testing.T) {
	m, _ := newSessionModel(t, script{sh: `sleep 30`, cmd: `ping -n 30 127.0.0.1 >nul`})
	a, _ := m.sessions.open("alpha", Launch{})
	b, _ := m.sessions.open("alpha", Launch{})
	if m.sessions.runner(a) == nil || m.sessions.runner(b) == nil {
		t.Fatal("both tabs should have a process")
	}
	if got := m.sessions.closeCurrent(); got != b || !m.st.Get("alpha").Open() {
		t.Fatalf("closed %v; alpha should stay open with one tab", got)
	}
	if got := m.sessions.closeCurrent(); got != a || m.st.Get("alpha").Open() || m.st.Active() != nil {
		t.Fatalf("closed %v; closing the last tab should leave alpha not open", got)
	}
	if m.sessions.runner(a) != nil || m.sessions.runner(b) != nil {
		t.Fatal("closed tabs must release their runners")
	}
	// Both processes were killed, so both report ending long before sleep 30 would.
	ended := map[*Session]bool{}
	deadline := time.After(5 * time.Second)
	for len(ended) < 2 {
		select {
		case msg := <-m.ev.ch:
			if e, ok := msg.(sessionEndedMsg); ok {
				ended[e.sess] = true
			}
		case <-deadline:
			t.Fatalf("processes still running after close: ended = %v", ended)
		}
	}
}
