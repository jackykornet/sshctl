package workspace

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sshctl/sshctl/internal/openssh"
)

// newResolvingModel is newTestModel whose Resolve answers from endpoints,
// counting its calls; an alias missing from endpoints fails.
func newResolvingModel(t *testing.T, w, h int, endpoints map[string]openssh.Endpoint) (*Model, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	m, err := New(Options{
		Catalog: &fakeCatalog{entries: []Entry{
			{Alias: "staging", Group: "web"},
			{Alias: "prod-web", Description: "public site", Group: "web"},
			{Alias: "db-main", Description: "postgres"},
		}, groups: []string{"web"}, identity: "/home/me/.ssh/id_ed25519"},
		Resolve: func(alias string) (openssh.Endpoint, error) {
			calls.Add(1)
			e, ok := endpoints[alias]
			if !ok {
				return openssh.Endpoint{}, errors.New("ssh -G failed")
			}
			return e, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m, calls
}

// paneHeader is the pane's header line, the screen's first line right of
// the list.
func paneHeader(m *Model) string {
	return strings.TrimSpace(ansi.Cut(strings.Split(screen(m), "\n")[0], m.paneOffset(), m.w))
}

func TestPaneHeaderShowsTheResolvedEndpointOnce(t *testing.T) {
	m, calls := newResolvingModel(t, 80, 10, map[string]openssh.Endpoint{
		"prod-web": {User: "deploy", HostName: "10.0.0.5", Port: "2222"},
		"db-main":  {User: "pg", HostName: "db.internal", Port: "22"},
	})
	m.st.SetCursor("prod-web")
	press(m, keyEnter)
	pump(t, m, func() bool { return paneHeader(m) == "prod-web · deploy@10.0.0.5:2222" })

	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab})
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	pump(t, m, func() bool { return paneHeader(m) == "db-main · pg@db.internal" })

	press(m, keyPrefix, text("{")[0]) // back to prod-web
	if got := paneHeader(m); got != "prod-web · deploy@10.0.0.5:2222" {
		t.Errorf("header = %q, want the cached endpoint", got)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("Resolve ran %d times, want once per Connection", n)
	}
}

func TestPaneHeaderShowsTheAliasWhenResolvingFails(t *testing.T) {
	m, calls := newResolvingModel(t, 80, 10, nil)
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	pump(t, m, func() bool { return calls.Load() == 1 && m.endpoints["db-main"].done })
	if got := paneHeader(m); got != "db-main" {
		t.Errorf("header = %q, want the alias alone", got)
	}
}

func TestPlusTabOpensANewTab(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	_, spans := m.tabBar(m.w - m.paneOffset())
	plus := spans[len(spans)-1]
	if plus.index != newTabSpan {
		t.Fatalf("last span = %+v, want [+]", plus)
	}
	m.Update(tea.MouseClickMsg{X: m.paneOffset() + plus.start + 1, Y: m.frame().tabRow, Button: tea.MouseLeft})
	if n := len(m.st.Get("db-main").Sessions); n != 2 {
		t.Fatalf("db-main has %d tabs after clicking [+], want 2", n)
	}
}

func TestStatusShowsTheBreadcrumbAndKey(t *testing.T) {
	m := newTestModel(t, 100, 10)
	if got := ansi.Strip(m.sessionStatus()); got != "no active connection · key: id_ed25519" {
		t.Errorf("status = %q", got)
	}
	m.st.SetCursor("prod-web")
	press(m, keyEnter)
	if got := ansi.Strip(m.sessionStatus()); got != "web / prod-web · key: id_ed25519" {
		t.Errorf("status = %q", got)
	}
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab})
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	if got := ansi.Strip(m.sessionStatus()); got != "ungrouped / db-main · key: id_ed25519" {
		t.Errorf("status = %q", got)
	}
}

func TestStatusHintsFollowFocus(t *testing.T) {
	m := newTestModel(t, 120, 10)
	if got, want := m.statusHints()[0].String(), "Enter open · / filter · a add · e edit · d delete · ^B ? help"; got != want {
		t.Errorf("list hints = %q, want %q", got, want)
	}
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	if got, want := m.statusHints()[0].String(), "^B: Tab list · n new · w close · [/] tab · s scroll · ? help"; got != want {
		t.Errorf("pane hints = %q, want %q", got, want)
	}
}

// At 80 columns the status line hints at five or more of the focused
// pane's keys (#89): the dim key gives way to them.
func TestStatusHintsShowFiveKeysAt80Columns(t *testing.T) {
	m := newTestModel(t, 80, 12)
	m.st.SetCursor("prod-web")
	count := func() int {
		rows := strings.Split(strings.TrimRight(screen(m), "\n"), "\n")
		status := rows[len(rows)-1]
		for _, l := range m.statusHints() {
			if strings.Contains(status, l.String()) {
				n := 0
				for _, it := range l.items {
					n += len(it.keys)
				}
				return n
			}
		}
		return 0
	}
	if n := count(); n < 5 {
		t.Errorf("list focus hints at %d keys, want 5 or more", n)
	}
	press(m, keyEnter)
	if n := count(); n < 5 {
		t.Errorf("pane focus hints at %d keys, want 5 or more", n)
	}
}

func TestFrameGolden(t *testing.T) {
	t.Run("space names that do not fit", func(t *testing.T) {
		m, err := New(Options{Catalog: &fakeCatalog{
			entries: []Entry{{Alias: "staging"}, {Alias: "db", Space: "customer-one-production"}},
			spaces:  []Space{{Name: "customer-one-production"}, {Name: "customer-two-staging", Number: 3}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		m.Update(tea.WindowSizeMsg{Width: 64, Height: 8})
		golden(t, "spaces-numbers", m)
	})
	t.Run("collapsed group", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		m.st.SetCursor("prod-web")
		press(m, keyEnter)                                     // prod-web 1, in web
		press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}) // back to the list
		m.st.SetCursorGroup("web")
		press(m, tea.KeyPressMsg{Code: tea.KeyLeft})
		golden(t, "collapsed", m)
	})
	t.Run("pane header", func(t *testing.T) {
		m, _ := newResolvingModel(t, 80, 10, map[string]openssh.Endpoint{
			"prod-web": {User: "deploy", HostName: "10.0.0.5", Port: "2222"},
		})
		m.st.SetCursor("prod-web")
		press(m, keyEnter)
		pump(t, m, func() bool { return m.endpoints["prod-web"].done })
		golden(t, "pane-header", m)
	})
	for _, h := range []int{8, 7, 6} {
		t.Run(fmt.Sprintf("short terminal %d rows", h), func(t *testing.T) {
			m := newSpaceModel(t, 80, h)
			m.st.SetCursor("staging")
			press(m, keyEnter)
			golden(t, fmt.Sprintf("short-%d", h), m)
		})
	}
}

// Short terminals drop the Space-bar rule, then the status rule, then the
// pane header, and always keep the tab line and the status bar.
func TestShortTerminalDropsTheFrameInOrder(t *testing.T) {
	for _, tt := range []struct {
		h                                int
		spaceRule, statusRule, paneTitle bool
	}{
		{h: 12, spaceRule: true, statusRule: true, paneTitle: true},
		{h: 8, spaceRule: false, statusRule: true, paneTitle: true},
		{h: 7, spaceRule: false, statusRule: false, paneTitle: true},
		{h: 6, spaceRule: false, statusRule: false, paneTitle: false},
		{h: 3, spaceRule: false, statusRule: false, paneTitle: false},
	} {
		m := newSpaceModel(t, 80, tt.h)
		f := m.frame()
		if f.spaceRule != tt.spaceRule || f.statusRule != tt.statusRule || f.paneHeader != tt.paneTitle {
			t.Errorf("height %d: frame = %+v", tt.h, f)
		}
		if !f.spaceBar {
			t.Errorf("height %d: Space bar hidden", tt.h)
		}
		if _, h := m.paneSize(); h < 1 {
			t.Errorf("height %d: pane height %d", tt.h, h)
		}
	}
}
