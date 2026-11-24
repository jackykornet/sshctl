package workspace

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var tunnelLaunch = Launch{Tunnel: true}

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
		m.Update(tunnelEndedMsg{run: run, proc: run.proc, code: 255, stderr: "prod-web: Permission denied (publickey).\n"})
		golden(t, "tunnels-failed", m)
	})
}

func TestTunnelStatusCountsRunningTunnels(t *testing.T) {
	m := newTunnelLayoutModel(t, 120, 12)
	if s := screen(m); !strings.Contains(s, "⇄ 3 tunnels") {
		t.Fatalf("status should count 3 running tunnels:\n%s", s)
	}
	run := m.tunnels.get("prod-web")
	m.Update(tunnelEndedMsg{run: run, proc: run.proc, code: 255})
	m.status = ""
	if s := screen(m); !strings.Contains(s, "⇄ 2 tunnels") {
		t.Fatalf("a failed tunnel is not counted:\n%s", s)
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
	proc := m.tunnels.get("web").proc
	press(m, keyPrefix, text("t")[0])
	if m.tunnels.get("web") != nil {
		t.Fatal("a second ^B t should stop web's tunnel")
	}
	if len(r.calls) != 1 {
		t.Errorf("Command calls = %+v, want only the start", r.calls)
	}
	if proc.cmd.ProcessState == nil {
		proc.cmd.Wait() // the stopped process must have been killed, so Wait returns
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
