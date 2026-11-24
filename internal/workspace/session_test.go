package workspace

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// script is a Session command for each platform: sh stands in for ssh on
// POSIX and cmd.exe on Windows. In sh the alias is $1. In cmd, {alias} is
// replaced with the alias, and delayed expansion (!var!) is on. A test with
// no cmd variant uses POSIX-only tools and is skipped on Windows.
type script struct{ sh, cmd string }

func (s script) argv(alias string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/d", "/q", "/v:on", "/s", "/c", strings.ReplaceAll(s.cmd, "{alias}", alias)}
	}
	return []string{"sh", "-c", s.sh, "sh", alias}
}

// newSessionModel returns a Workspace whose Sessions run s in place of ssh.
func newSessionModel(t *testing.T, s script) (*Model, *[]string) {
	t.Helper()
	if runtime.GOOS == "windows" && s.cmd == "" {
		t.Skip("uses POSIX-only tools")
	}
	f := &fakeCatalog{entries: []Entry{{Alias: "alpha"}, {Alias: "beta"}}}
	m, err := New(Options{
		Catalog: f,
		Command: func(alias string, _ Launch) ([]string, error) {
			return s.argv(alias), nil
		},
		ScrollbackLines: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return m, &f.opened
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
	m, opened := newSessionModel(t, script{
		sh:  `printf 'hello %s\n' "$1"; while read -r line; do printf 'got:%s\n' "$line"; done`,
		cmd: `echo hello {alias}& for /l %i in (1,1,1000) do @(set line=& set /p line=& echo got:!line!)`,
	})
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
		return strings.Contains(screen(m), "[2 ssh ●]") && strings.Contains(screen(m), "hello alpha")
	})
	if len(*opened) != 1 {
		t.Fatalf("OnOpen calls after second tab = %v", *opened)
	}
}

func TestSessionEndsAndReconnects(t *testing.T) {
	m, _ := newSessionModel(t, script{sh: `echo run; exit 3`, cmd: `echo run& exit 3`})
	press(m, keyEnter)
	sess := m.st.Active().CurrentSession()
	pump(t, m, func() bool { return sess.Ended })
	if sess.ExitCode != 3 || !strings.Contains(screen(m), "session ended (exit 3)") || !strings.Contains(screen(m), "run") {
		t.Fatalf("exit=%d screen:\n%s", sess.ExitCode, screen(m))
	}
	if !m.st.Get("alpha").Open() {
		t.Fatal("ended tab keeps the connection open")
	}
	old := m.sessions.runner(sess)
	press(m, text("r")...)
	if sess.Ended || m.sessions.runner(sess) == old {
		t.Fatal("r should start a new process in the same tab")
	}
	pump(t, m, func() bool { return sess.Ended })
	press(m, text("x")...)
	if m.st.Get("alpha").Open() || m.sessions.runner(sess) != nil {
		t.Fatal("x should close the ended tab and release its process")
	}
}

func TestStaleEndedMessageIsIgnoredAfterReconnect(t *testing.T) {
	m, _ := newSessionModel(t, script{sh: `sleep 30`, cmd: `ping -n 30 127.0.0.1 >nul`})
	press(m, keyEnter)
	sess := m.st.Active().CurrentSession()
	m.Update(sessionEndedMsg{sess: sess, r: &runner{}, code: 1})
	if sess.Ended {
		t.Fatal("an ended message from a replaced runner must not end the Session")
	}
}

func TestPrefixTwiceSendsLiteralPrefix(t *testing.T) {
	// od prints the byte value of what it receives: ^B is 002.
	m, _ := newSessionModel(t, script{sh: `stty raw -echo; head -c 1 | od -An -c`})
	press(m, keyEnter)
	time.Sleep(200 * time.Millisecond) // let stty run before sending
	press(m, keyPrefix, keyPrefix)
	pump(t, m, func() bool { return strings.Contains(screen(m), "002") })
}

func TestScrollback(t *testing.T) {
	m, _ := newSessionModel(t, script{
		sh:  `i=1; while [ $i -le 40 ]; do echo "line $i"; i=$((i+1)); done; sleep 30`,
		cmd: `(for /l %i in (1,1,40) do @echo line %i) & ping -n 30 127.0.0.1 >nul`,
	})
	press(m, keyEnter)
	pump(t, m, func() bool { return strings.Contains(screen(m), "line 40") })
	if strings.Contains(screen(m), "line 1 ") {
		t.Fatal("line 1 should have scrolled off")
	}
	press(m, keyPrefix, text("s")[0])
	press(m, text("g")...)
	if !m.scrolling() || !strings.Contains(screen(m), "line 1 ") {
		t.Fatalf("g in scroll mode should show the top of history:\n%s", screen(m))
	}
	press(m, text("q")...)
	if m.scrolling() {
		t.Fatal("q leaves scroll mode")
	}
}

func TestPaneSizeReachesPseudoTerminal(t *testing.T) {
	// The Session prints the terminal size, waits for Enter, then prints it again.
	m, _ := newSessionModel(t, script{
		sh:  `stty size; read x; stty size; sleep 30`,
		cmd: `mode con& set /p x=& mode con& ping -n 30 127.0.0.1 >nul`,
	})
	reports := func(rows, cols int) func() bool {
		var want *regexp.Regexp
		if runtime.GOOS == "windows" {
			want = regexp.MustCompile(fmt.Sprintf(`Lines:\s+%d\s.*\n.*Columns:\s+%d\s`, rows, cols))
		} else {
			want = regexp.MustCompile(fmt.Sprintf(`(?m)^.*│%d %d\s`, rows, cols))
		}
		return func() bool { return want.MatchString(screen(m)) }
	}
	press(m, keyEnter)
	// 80 wide minus the 28-column list and separator, 12 high minus the
	// pane header, tab line, status rule and status bar.
	pump(t, m, reports(8, 51))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	press(m, keyEnter)
	pump(t, m, reports(16, 71))
}

func TestOSC52CopiesToClipboard(t *testing.T) {
	m, _ := newSessionModel(t, script{sh: `printf '\033]52;c;aGVsbG8=\007'; sleep 30`})
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

func TestBackgroundOutputMarksTabUntilShown(t *testing.T) {
	m, _ := newSessionModel(t, script{
		sh:  `sleep 0.3; echo later; sleep 30`,
		cmd: `ping -n 2 127.0.0.1 >nul& echo later& ping -n 30 127.0.0.1 >nul`,
	})
	press(m, keyEnter)
	press(m, keyPrefix, text("n")[0])
	c := m.st.Active()
	bg, shown := c.Sessions[0], c.Sessions[1]
	pump(t, m, func() bool { return bg.Unseen && strings.Contains(screen(m), "later") })
	if shown.Unseen {
		t.Fatal("output in the shown tab must not mark it")
	}
	press(m, keyPrefix, text("[")[0])
	if bg.Unseen || c.Unseen() {
		t.Fatal("switching to the tab clears its marker")
	}
}

func TestBellMarksBackgroundConnection(t *testing.T) {
	m, _ := newSessionModel(t, script{
		sh:  `sleep 0.3; printf '\a'; sleep 30`,
		cmd: "ping -n 2 127.0.0.1 >nul& echo \a& ping -n 30 127.0.0.1 >nul",
	})
	press(m, keyEnter)                                                        // alpha
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}, keyDown, keyEnter) // beta
	alpha := m.st.Get("alpha")
	pump(t, m, func() bool { return alpha.Unseen() })
	if m.st.Get("beta").Unseen() {
		t.Fatal("the shown tab's bell must not mark it")
	}
	press(m, keyPrefix, text("{")[0]) // back to alpha
	if alpha.Unseen() {
		t.Fatal("showing alpha's only tab clears its row marker")
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
