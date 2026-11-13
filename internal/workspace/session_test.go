package workspace

import (
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newSessionModel returns a Workspace whose Sessions run script with sh in
// place of ssh. The alias is passed as $1.
func newSessionModel(t *testing.T, script string) (*Model, *[]string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("session tests use sh")
	}
	var opened []string
	m, err := New(Options{
		Load: func() (Snapshot, error) {
			return Snapshot{Entries: []Entry{{Alias: "alpha"}, {Alias: "beta"}}}, nil
		},
		Command: func(alias string, _ bool) ([]string, error) {
			return []string{"sh", "-c", script, "sh", alias}, nil
		},
		OnOpen: func(alias string) error {
			opened = append(opened, alias)
			return nil
		},
		ScrollbackLines: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return m, &opened
}

// pump feeds runner messages into the model until cond holds.
func pump(t *testing.T, m *Model, cond func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	msgs := make(chan tea.Msg)
	for !cond() {
		go func() { msgs <- m.ev.listen()() }()
		select {
		case msg := <-msgs:
			m.Update(msg)
		case <-deadline:
			t.Fatalf("timed out; screen:\n%s", ansi.Strip(m.render()))
		}
	}
}

func screen(m *Model) string { return ansi.Strip(m.render()) }

func TestSessionShowsOutputAndTakesInput(t *testing.T) {
	m, opened := newSessionModel(t, `printf 'hello %s\n' "$1"; while read -r line; do printf 'got:%s\n' "$line"; done`)
	press(m, keyEnter)
	pump(t, m, func() bool { return strings.Contains(screen(m), "hello alpha") })
	press(m, text("ping")...)
	press(m, keyEnter)
	pump(t, m, func() bool { return strings.Contains(screen(m), "got:ping") })
	if len(*opened) != 1 || (*opened)[0] != "alpha" {
		t.Fatalf("OnOpen calls = %v", *opened)
	}
	press(m, keyPrefix, text("n")[0]) // a second tab must not record history again
	pump(t, m, func() bool {
		return strings.Contains(screen(m), "alpha 2") && strings.Contains(screen(m), "hello alpha")
	})
	if len(*opened) != 1 {
		t.Fatalf("OnOpen calls after second tab = %v", *opened)
	}
}

func TestSessionEndsAndReconnects(t *testing.T) {
	m, _ := newSessionModel(t, `echo run; exit 3`)
	press(m, keyEnter)
	sess := m.st.Active().CurrentSession()
	pump(t, m, func() bool { return sess.Ended })
	if sess.ExitCode != 3 || !strings.Contains(screen(m), "session ended (exit 3)") || !strings.Contains(screen(m), "run") {
		t.Fatalf("exit=%d screen:\n%s", sess.ExitCode, screen(m))
	}
	if !m.st.Get("alpha").Open() {
		t.Fatal("ended tab keeps the connection open")
	}
	old := m.runners[sess]
	press(m, text("r")...)
	if sess.Ended || m.runners[sess] == old {
		t.Fatal("r should start a new process in the same tab")
	}
	pump(t, m, func() bool { return sess.Ended })
	press(m, text("x")...)
	if m.st.Get("alpha").Open() || len(m.runners) != 0 {
		t.Fatal("x should close the ended tab and release its process")
	}
}

func TestStaleEndedMessageIsIgnoredAfterReconnect(t *testing.T) {
	m, _ := newSessionModel(t, `sleep 30`)
	press(m, keyEnter)
	sess := m.st.Active().CurrentSession()
	m.Update(sessionEndedMsg{sess: sess, r: &runner{}, code: 1})
	if sess.Ended {
		t.Fatal("an ended message from a replaced runner must not end the Session")
	}
}

func TestPrefixTwiceSendsLiteralPrefix(t *testing.T) {
	// od prints the byte value of what it receives: ^B is 002.
	m, _ := newSessionModel(t, `stty raw -echo; head -c 1 | od -An -c`)
	press(m, keyEnter)
	time.Sleep(200 * time.Millisecond) // let stty run before sending
	press(m, keyPrefix, keyPrefix)
	pump(t, m, func() bool { return strings.Contains(screen(m), "002") })
}

func TestScrollbackAndResize(t *testing.T) {
	m, _ := newSessionModel(t, `i=1; while [ $i -le 40 ]; do echo "line $i"; i=$((i+1)); done; stty size; sleep 30`)
	press(m, keyEnter)
	// Wait for stty too: output can arrive after "line 40" is on screen.
	pump(t, m, func() bool { return strings.Contains(screen(m), "line 40") && strings.Contains(screen(m), "10 51") })
	if strings.Contains(screen(m), "line 1\n") {
		t.Fatal("line 1 should have scrolled off")
	}
	press(m, keyPrefix, text("[")[0])
	press(m, text("g")...)
	if !m.scrolling || !strings.Contains(screen(m), "line 1 ") {
		t.Fatalf("g in scroll mode should show the top of history:\n%s", screen(m))
	}
	press(m, text("q")...)
	if m.scrolling {
		t.Fatal("q leaves scroll mode")
	}
	// 80 wide minus the 28-column list and separator, 12 high minus tab bar and status bar.
	if !strings.Contains(screen(m), "10 51") {
		t.Fatalf("stty should report the pane size 10x51:\n%s", screen(m))
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	_, r := m.currentRunner()
	if w, h := r.term.Size(); w != 71 || h != 18 {
		t.Fatalf("terminal size after resize = %dx%d, want 71x18", w, h)
	}
}

func TestOSC52CopiesToClipboard(t *testing.T) {
	m, _ := newSessionModel(t, `printf '\033]52;c;aGVsbG8=\007'; sleep 30`)
	press(m, keyEnter)
	msgs := make(chan tea.Msg, 1)
	go func() { msgs <- m.ev.listen()() }()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if c, ok := msg.(clipboardMsg); ok {
				if c.text != "hello" {
					t.Fatalf("clipboard = %q", c.text)
				}
				return
			}
			go func() { msgs <- m.ev.listen()() }()
		case <-deadline:
			t.Fatal("no clipboard message")
		}
	}
}

func TestParseOSC52(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"52;c;aGVsbG8=", "hello", true},
		{"c;aGVsbG8=", "hello", true},
		{"52;c;?", "", false},
		{"52;c;!!", "", false},
		{"aGVsbG8=", "", false},
	}
	for _, tt := range tests {
		got, ok := parseOSC52([]byte(tt.in))
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseOSC52(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
