package workspace

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite golden files")

func newTestModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m, err := New(Options{Catalog: &fakeCatalog{
		entries: []Entry{
			{Alias: "staging", Group: "web"},
			{Alias: "prod-web", Description: "public site", Group: "web"},
			{Alias: "db-main", Description: "postgres"},
			{Alias: "old-box", Description: "retired", Orphaned: true},
		},
		groups:   []string{"web"},
		identity: "/home/me/.ssh/id_ed25519",
	}})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func press(m *Model, keys ...tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(k)
	}
	return cmd
}

func text(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

var (
	keyEnter  = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyDown   = tea.KeyPressMsg{Code: tea.KeyDown}
	keyPrefix = tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
)

func golden(t *testing.T, name string, m *Model) {
	t.Helper()
	got := ansi.Strip(m.render())
	for i, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w != m.w {
			t.Errorf("line %d is %d columns wide, want %d: %q", i, w, m.w, line)
		}
	}
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/workspace -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestLayoutGolden(t *testing.T) {
	t.Run("initial", func(t *testing.T) {
		golden(t, "initial", newTestModel(t, 80, 10))
	})
	t.Run("open connection stays in its group", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, keyDown, keyEnter)       // prod-web, under the web header
		press(m, keyPrefix, text("n")[0]) // second tab
		golden(t, "open", m)
	})
	t.Run("filter", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, text("/web")...)
		golden(t, "filter", m)
	})
	t.Run("narrow terminal hides list", func(t *testing.T) {
		m := newTestModel(t, 50, 6)
		m.st.SetCursor("db-main")
		press(m, keyEnter)
		golden(t, "narrow", m)
	})
	t.Run("delete group asks", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, text("d")...) // the cursor starts on web's header
		golden(t, "group-delete", m)
	})
	t.Run("rename group prompt", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		press(m, text("e")...)
		press(m, text("-1")...)
		golden(t, "group-rename", m)
	})
	t.Run("help", func(t *testing.T) {
		m := newTestModel(t, 80, 24)
		press(m, keyPrefix, text("?")[0])
		golden(t, "help", m)
	})
	t.Run("help scrolled", func(t *testing.T) {
		m := newTestModel(t, 80, 12)
		press(m, keyPrefix, text("?")[0])
		press(m, keyDown)
		golden(t, "help-scrolled", m)
	})
	t.Run("add form", func(t *testing.T) {
		m := newTestModel(t, 80, 16)
		m.st.SetCursor("staging") // on a header, a adds a Group
		press(m, text("a")...)
		golden(t, "add-form", m)
	})
	t.Run("move prompt", func(t *testing.T) {
		m := newTestModel(t, 80, 10)
		m.st.SetCursor("staging")
		press(m, keyPrefix, text("m")[0])
		golden(t, "move-prompt", m)
	})
}

func TestActivityMarkersGolden(t *testing.T) {
	m := newTestModel(t, 80, 8)
	press(m, keyDown, keyEnter) // prod-web 1
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab})
	m.st.SetCursor("db-main")
	press(m, keyEnter) // db-main 1
	press(m, keyPrefix, text("n")[0], keyPrefix, text("n")[0])
	prod, db := m.st.Get("prod-web"), m.st.Get("db-main")
	m.st.MarkActivity(prod.Sessions[0])
	m.st.MarkActivity(db.Sessions[0])
	m.st.MarkActivity(db.Sessions[1])
	endSession(m, db.Sessions[1], 1)
	m.st.MarkActivity(db.Sessions[2]) // shown: never marked
	golden(t, "activity", m)

	alt := tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}
	press(m, alt, alt) // through db-main 2 to db-main 1
	golden(t, "activity-cleared", m)
}

func TestTabBarScrollsToCurrentTab(t *testing.T) {
	m := newTestModel(t, 70, 6)
	m.st.SetCursor("db-main")
	press(m, keyEnter) // db-main 1
	for range 6 {
		press(m, keyPrefix, text("n")[0])
	}
	bar, spans := m.tabBar(m.w - m.paneOffset())
	bar = ansi.Strip(bar)
	if !strings.HasPrefix(bar, "‹") || !strings.Contains(bar, "db-main 7") {
		t.Fatalf("tab bar should scroll to show tab 7: %q", bar)
	}
	if spans[len(spans)-1].index != 6 {
		t.Fatalf("last visible tab = %d, want 6", spans[len(spans)-1].index)
	}
}

func TestQuitConfirmsWhenSessionsAreLive(t *testing.T) {
	m := newTestModel(t, 80, 10)
	if cmd := press(m, text("q")...); !isQuit(cmd) {
		t.Fatal("q with no sessions should quit immediately")
	}
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	if cmd := press(m, keyPrefix, text("q")[0]); cmd != nil || m.confirm() == nil {
		t.Fatal("quit with a live session should ask first")
	}
	if cmd := press(m, text("n")...); cmd != nil || m.confirm() != nil {
		t.Fatal("answering n should cancel")
	}
	press(m, keyPrefix, text("q")[0])
	if cmd := press(m, text("y")...); !isQuit(cmd) {
		t.Fatal("answering y should quit")
	}
}

func TestCloseTabConfirmsOnlyWhenLive(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	press(m, keyPrefix, text("w")[0])
	if m.confirm() == nil {
		t.Fatal("closing a live tab should ask first")
	}
	press(m, text("y")...)
	if m.st.Active() != nil || m.focus != focusList {
		t.Fatal("closing the last tab should return focus to the list")
	}
	press(m, keyEnter)
	c := m.st.Active()
	endSession(m, c.CurrentSession(), 1)
	press(m, keyPrefix, text("w")[0])
	if m.confirm() != nil || m.st.Active() != nil {
		t.Fatal("closing an ended tab should not ask")
	}
}

func TestEnterOnOrphanedConnectionShowsError(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("old-box")
	press(m, keyEnter)
	if m.st.Active() != nil || !m.statusErr || !strings.Contains(m.status, "missing") {
		t.Fatalf("status = %q, active = %v", m.status, m.st.Active())
	}
}

func TestClickOpenConnectionActivatesIt(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter)                                     // opens db-main (row 4), focus moves to the pane
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}) // focus back to the list
	m.st.SetCursor("prod-web")
	press(m, keyEnter) // opens prod-web (row 1), now active
	m.Update(tea.MouseClickMsg{X: 3, Y: 4, Button: tea.MouseLeft})
	if m.st.Active().Alias != "db-main" || m.focus != focusPane {
		t.Fatalf("active = %s focus = %v", m.st.Active().Alias, m.focus)
	}
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
