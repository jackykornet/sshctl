package workspace

import (
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var tunnelLaunch = Launch{Kind: LaunchTunnel}

// newTunnelLayoutModel returns a w×h Workspace with no processes, whose
// prod-web and db-main (default Space) and vps (Space 2) have Tunnels, and
// whose tunnels have been started: vps's, then prod-web's and db-main's.
func newTunnelLayoutModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m, err := New(Options{Catalog: &fakeCatalog{
		entries: []Entry{
			{Alias: "staging", Group: "web"},
			{Alias: "prod-web", Description: "public site", Group: "web", Tunnels: []Tunnel{panelTunnel}},
			{Alias: "db-main", Description: "postgres", Tunnels: []Tunnel{{LocalPort: 5433, RemoteHost: "localhost", RemotePort: 5432}}},
			{Alias: "vps", Space: "vps", Tunnels: []Tunnel{{Name: "panel", LocalPort: 8084, RemoteHost: "localhost", RemotePort: 8083}}},
		},
		groups:   []string{"web"},
		spaces:   []Space{{Name: "vps"}},
		identity: "/home/me/.ssh/id_ed25519",
	}})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	press(m, text("2")...)
	startTunnels(m, "vps")
	press(m, text("1")...)
	startTunnels(m, "prod-web", "db-main")
	m.status = ""
	return m
}

func TestTunnelsLayoutGolden(t *testing.T) {
	t.Run("running", func(t *testing.T) {
		m := newTunnelLayoutModel(t, 80, 12)
		golden(t, "tunnels", m)
	})
	t.Run("cursor on a tunnel row", func(t *testing.T) {
		m := newTunnelLayoutModel(t, 80, 12)
		m.st.SetCursor("db-main")
		press(m, text("jj")...)
		golden(t, "tunnels-cursor", m)
	})
	t.Run("failed", func(t *testing.T) {
		m := newTunnelLayoutModel(t, 80, 12)
		run := m.tunnels.get("prod-web")
		m.Update(tunnelExitedMsg{run: run, proc: run.proc, code: 255, stderr: "prod-web: Permission denied (publickey).\n"})
		golden(t, "tunnels-failed", m)
	})
}

func TestTunnelStatusCountsRunningTunnels(t *testing.T) {
	m := newTunnelLayoutModel(t, 120, 12)
	if s := screen(m); !strings.Contains(s, "⇄ 3 tunnels") {
		t.Fatalf("status should count 3 running tunnels:\n%s", s)
	}
	run := m.tunnels.get("prod-web")
	m.Update(tunnelExitedMsg{run: run, proc: run.proc, code: 255})
	m.status = ""
	if s := screen(m); !strings.Contains(s, "⇄ 2 tunnels") {
		t.Fatalf("a failed tunnel is not counted:\n%s", s)
	}
}

// newTunnelProcessModel returns a Workspace whose tunnels run s in place of
// ssh, over tunnelEntries.
func newTunnelProcessModel(t *testing.T, s script) *Model {
	t.Helper()
	if runtime.GOOS == "windows" && s.cmd == "" {
		t.Skip("uses POSIX-only tools")
	}
	m, err := New(Options{
		Catalog: &fakeCatalog{entries: tunnelEntries()},
		Command: func(alias string, l Launch) ([]string, error) {
			if l.Kind != LaunchTunnel {
				t.Errorf("Command(%s, %+v), want a tunnel launch", alias, l)
			}
			return s.argv(alias), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	return m
}

func TestAFailedTunnelShowsSshsErrorUntilRestarted(t *testing.T) {
	m := newTunnelProcessModel(t, script{
		sh:  `echo "$1: Permission denied (publickey,password)." >&2; exit 255`,
		cmd: `echo {alias}: Permission denied (publickey,password).>&2& exit 255`,
	})
	startTunnels(m, "web")
	run := m.tunnels.get("web")
	pump(t, m, func() bool { return run.failed() })
	for _, want := range []string{"✕", "web tunnel failed: web: Permission denied (publickey,password)."} {
		if !strings.Contains(screen(m), want) {
			t.Errorf("screen lacks %q:\n%s", want, screen(m))
		}
	}
	if !strings.Contains(run.err, batchHint) {
		t.Errorf("error %q lacks the BatchMode hint", run.err)
	}
	if m.tunnels.count() != 0 || strings.Contains(screen(m), "⇄ 2 tunnels") {
		t.Error("a failed tunnel is not running")
	}
	m.st.SetCursor("web")
	press(m, text("j")...)
	press(m, text("r")...)
	if run.failed() {
		t.Fatal("r should restart the failed tunnel")
	}
}

// The BatchMode hint is for a tunnel that needed a prompt, not for a key the
// server rejected.
func TestATunnelFailureHintsOnlyAtAPrompt(t *testing.T) {
	for _, tc := range []struct {
		stderr string
		hint   bool
	}{
		{"web: Permission denied (publickey).", false},
		{"web: Permission denied (publickey,password).", true},
		{"web: Permission denied (publickey,keyboard-interactive).", true},
		{"Host key verification failed.", true},
		{"Load key \"/home/me/.ssh/id_ed25519\": incorrect passphrase supplied to decrypt private key", true},
		{"bind [127.0.0.1]:8083: Address already in use", false},
	} {
		m, _ := newRecordingModel(t, tunnelEntries())
		startTunnels(m, "web")
		run := m.tunnels.get("web")
		m.Update(tunnelExitedMsg{run: run, proc: run.proc, code: 255, stderr: tc.stderr + "\n"})
		if got := strings.Contains(run.err, batchHint); got != tc.hint {
			t.Errorf("%q: error %q, hint %v, want %v", tc.stderr, run.err, got, tc.hint)
		}
	}
}

func TestAStoppedTunnelIsNotReportedAsFailed(t *testing.T) {
	m := newTunnelProcessModel(t, script{sh: `exec sleep 30`, cmd: `ping -n 30 127.0.0.1 >nul`})
	startTunnels(m, "web")
	run := m.tunnels.get("web")
	press(m, keyPrefix, text("t")[0])
	m.Update(tunnelExitedMsg{run: run, proc: run.proc, code: -1})
	if m.statusErr || run.failed() {
		t.Errorf("a stopped tunnel's exit is no failure: status %q", m.status)
	}
}

func TestQuitAsksOnlyWhileTunnelsRunAndStopsThem(t *testing.T) {
	m := newTunnelProcessModel(t, script{sh: `exec sleep 30`, cmd: `ping -n 30 127.0.0.1 >nul`})
	if cmd := press(m, text("q")...); cmd == nil || m.mode.modal != nil {
		t.Fatal("with no tunnel, q quits at once")
	}
	startTunnels(m, "web")
	proc := m.tunnels.get("web").proc
	press(m, text("q")...)
	if s, _ := m.mode.modal.status(); !strings.Contains(s, "2 tunnels running, quit anyway?") {
		t.Fatalf("quit asks %q", s)
	}
	press(m, text("y")...)
	if m.tunnels.get("web") != nil {
		t.Fatal("confirming the quit stops every tunnel")
	}
	select {
	case <-proc.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the tunnel process is still running after the quit")
	}
}

func TestAClickSelectsATunnelRow(t *testing.T) {
	m := newTunnelLayoutModel(t, 80, 12)
	m.Update(click(3, 10, tea.MouseLeft)) // db-main's row; see tunnels.golden
	m.Update(release(3, 10))
	if alias, ok := m.tunnelTarget(); !ok || alias != "db-main" || m.tunnelCursor != 2 {
		t.Fatalf("clicked tunnel row %d (%s), want db-main's", m.tunnelCursor, alias)
	}
	m.Update(click(3, 3, tea.MouseLeft)) // staging, back in the list
	m.Update(release(3, 3))
	if _, ok := m.tunnelTarget(); ok {
		t.Error("a click on the list takes the cursor out of the Tunnels section")
	}
	if c := m.st.Cursor(); c == nil || c.Alias != "staging" {
		t.Errorf("cursor on %v, want staging", c)
	}
}

func TestSwitchingSpaceKeepsTunnelsRunning(t *testing.T) {
	m := newTunnelLayoutModel(t, 80, 12)
	press(m, text("2")...)
	press(m, text("1")...)
	for _, a := range []string{"vps", "prod-web", "db-main"} {
		if !m.tunnels.running(a) {
			t.Errorf("%s's tunnel stopped on a Space switch", a)
		}
	}
}

// tunnelEntries is web with the Tunnels panel and cockpit, and db with none.
func tunnelEntries() []Entry {
	return []Entry{
		{Alias: "web", Tunnels: []Tunnel{panelTunnel, cockpitTunnel}},
		{Alias: "db"},
	}
}

func TestPrefixTStartsTheCursorsTunnelsWithoutOpeningIt(t *testing.T) {
	m, r := newRecordingModel(t, tunnelEntries())
	m.st.SetCursor("web")
	press(m, keyPrefix, text("t")[0])
	if want := (commandCall{"web", tunnelLaunch}); len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("Command calls = %+v, want one %+v", r.calls, want)
	}
	if !m.tunnels.running("web") {
		t.Fatal("web's tunnel should be running")
	}
	if c := m.st.Get("web"); c.Open() || m.st.Active() != nil {
		t.Error("a tunnel must not make its Connection open or active")
	}
	if len(r.opened) != 0 {
		t.Errorf("History recorded %v, want nothing for a tunnel", r.opened)
	}
	if m.focus != focusList {
		t.Error("starting a tunnel keeps focus on the list")
	}
}

func TestPrefixTStopsARunningTunnel(t *testing.T) {
	m, r := newRecordingModel(t, tunnelEntries())
	m.st.SetCursor("web")
	press(m, keyPrefix, text("t")[0])
	press(m, keyPrefix, text("t")[0])
	if m.tunnels.get("web") != nil {
		t.Fatal("a second ^B t should stop web's tunnel")
	}
	if len(r.calls) != 1 {
		t.Errorf("Command calls = %+v, want only the start", r.calls)
	}
}

func TestPrefixTWithNoTunnelsOpensTheFormOnTunnels(t *testing.T) {
	m, r := newRecordingModel(t, tunnelEntries())
	m.st.SetCursor("db")
	press(m, keyPrefix, text("t")[0])
	f := m.form()
	if f == nil || f.fields[f.focus].id != fieldTunnels {
		t.Fatalf("want the edit form focused on Tunnels, got %+v", f)
	}
	if len(r.calls) != 0 {
		t.Errorf("Command calls = %+v, want none", r.calls)
	}
}

// startTunnels starts the tunnels of aliases with ^B t, in order.
func startTunnels(m *Model, aliases ...string) {
	for _, a := range aliases {
		m.st.SetCursor(a)
		press(m, keyPrefix, text("t")[0])
	}
}

func TestTheCursorMovesIntoTheTunnelsSectionAndBack(t *testing.T) {
	m, _ := newRecordingModel(t, tunnelEntries())
	startTunnels(m, "web")
	m.st.SetCursor("web") // the last list row
	press(m, text("j")...)
	if alias, ok := m.tunnelTarget(); !ok || alias != "web" || m.tunnelCursor != 0 {
		t.Fatalf("j from the last row should reach web's first tunnel row, cursor %d", m.tunnelCursor)
	}
	press(m, keyDown, keyDown)
	if m.tunnelCursor != 1 {
		t.Fatalf("the cursor should stop on the last tunnel row, at %d", m.tunnelCursor)
	}
	press(m, text("kk")...)
	if _, ok := m.tunnelTarget(); ok || m.st.Cursor() == nil || m.st.Cursor().Alias != "web" {
		t.Fatalf("k from the first tunnel row goes back to the list's last row")
	}
}

func TestXOnATunnelRowStopsItAndRRestartsIt(t *testing.T) {
	m, r := newRecordingModel(t, tunnelEntries())
	startTunnels(m, "web")
	m.st.SetCursor("web")
	press(m, text("j")...)
	old := m.tunnels.get("web").proc
	press(m, text("r")...)
	if run := m.tunnels.get("web"); run == nil || run.proc == old || len(r.calls) != 2 {
		t.Fatalf("r should start a new process for web, calls %+v", r.calls)
	}
	press(m, text("x")...)
	if m.tunnels.get("web") != nil {
		t.Fatal("x should stop web's tunnel")
	}
	if _, ok := m.tunnelTarget(); ok {
		t.Error("with no tunnel left the cursor goes back to the list")
	}
	if c := m.st.Cursor(); c == nil || c.Alias != "web" {
		t.Errorf("cursor on %v, want web", c)
	}
}

// r cannot restart a tunnel whose Connection was renamed or deleted, so it
// says so and leaves the row for x to stop.
func TestRRefusesATunnelWhoseConnectionIsGone(t *testing.T) {
	m, r := newRecordingModel(t, tunnelEntries())
	startTunnels(m, "web")
	r.entries = []Entry{{Alias: "web2", Tunnels: []Tunnel{panelTunnel}}, {Alias: "db"}}
	m.reload("", "")
	m.st.SetCursor("web2")
	press(m, text("j")...)
	if alias, ok := m.tunnelTarget(); !ok || alias != "web" {
		t.Fatalf("cursor on tunnel row %q, want web's", alias)
	}
	proc := m.tunnels.get("web").proc
	press(m, text("r")...)
	if run := m.tunnels.get("web"); run == nil || run.proc != proc {
		t.Fatal("r must leave web's tunnel as it is")
	}
	if len(r.calls) != 1 {
		t.Errorf("Command calls = %+v, want only the first start", r.calls)
	}
	if !m.statusErr || !strings.Contains(m.status, "web is no longer in the connection list") {
		t.Errorf("status %q (error %v), want it to say web is gone", m.status, m.statusErr)
	}
	if alias, ok := m.tunnelTarget(); !ok || alias != "web" {
		t.Errorf("the cursor left web's tunnel row, now %q", alias)
	}
}

func TestListKeysWaitForAConnectionRowInTheTunnelsSection(t *testing.T) {
	m, r := newRecordingModel(t, tunnelEntries())
	startTunnels(m, "web")
	m.st.SetCursor("web")
	press(m, text("j")...)
	press(m, keyEnter)
	press(m, text("d")...)
	if m.st.Get("web").Open() || len(r.calls) != 1 || m.mode.modal != nil {
		t.Errorf("Enter and d on a tunnel row must not act on the list: calls %+v", r.calls)
	}
}
