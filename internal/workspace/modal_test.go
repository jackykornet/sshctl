package workspace

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var keyEsc = tea.KeyPressMsg{Code: tea.KeyEscape}

// modals opens each modal from the Workspace in newStoreModel with web
// under the cursor.
var modals = []struct {
	name string
	open func(m *Model)
}{
	{"form", func(m *Model) { press(m, text("e")...) }},
	{"move prompt", func(m *Model) { press(m, keyPrefix, text("m")[0]) }},
	{"confirm", func(m *Model) { press(m, text("d")...) }},
	{"help", func(m *Model) { press(m, keyPrefix, text("?")[0]) }},
}

func newModalModel(t *testing.T) (*Model, *fakeCatalog) {
	t.Helper()
	f := &fakeCatalog{entries: storeEntries()}
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	return m, f
}

func TestPrefixQuitWorksInLineModals(t *testing.T) {
	for _, tt := range modals[:2] {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newModalModel(t)
			tt.open(m)
			if cmd := press(m, keyPrefix, text("q")[0]); !isQuit(cmd) {
				t.Fatal("^B q with no live Sessions should quit")
			}
		})
	}
}

func TestPrefixQuitAsksThenReturnsToTheModal(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter) // a live Session
	press(m, keyPrefix, text("m")[0])
	press(m, keyClear)
	press(m, text("ops")...)
	if cmd := press(m, keyPrefix, text("q")[0]); cmd != nil || m.confirm() == nil {
		t.Fatal("^B q with a live Session should ask first")
	}
	press(m, text("n")...)
	if !strings.Contains(screen(m), "move db-main to group: ops█") {
		t.Fatalf("answering n should return to the move prompt:\n%s", screen(m))
	}
	press(m, keyPrefix, text("q")[0])
	if cmd := press(m, text("y")...); !isQuit(cmd) {
		t.Fatal("answering y should quit")
	}
}

func TestOtherPrefixCommandsDoNothingInLineModals(t *testing.T) {
	for _, tt := range modals[:2] {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newModalModel(t)
			tt.open(m)
			before := screen(m)
			press(m, keyPrefix)
			if !strings.Contains(screen(m), "^B …") {
				t.Errorf("the status line should show the armed prefix:\n%s", screen(m))
			}
			press(m, keyPrefix) // ^B ^B
			for _, k := range []tea.KeyPressMsg{text("?")[0], keyPrefix, text("b")[0], text("m")[0], text("x")[0]} {
				press(m, keyPrefix, k)
			}
			if got := screen(m); got != before {
				t.Fatalf("prefix commands changed the screen\n--- before ---\n%s\n--- after ---\n%s", before, got)
			}
		})
	}
}

func TestMouseIsIgnoredWhileAModalIsOpen(t *testing.T) {
	for _, tt := range modals {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newModalModel(t)
			tt.open(m)
			before := screen(m)
			m.Update(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}) // mine
			m.Update(tea.MouseReleaseMsg{X: 3, Y: 2, Button: tea.MouseLeft})
			m.Update(tea.MouseWheelMsg{X: 3, Y: 2, Button: tea.MouseWheelDown})
			if got := screen(m); got != before {
				t.Fatalf("the mouse changed the screen\n--- before ---\n%s\n--- after ---\n%s", before, got)
			}
		})
	}
}

func TestPasteGoesToTheModal(t *testing.T) {
	tests := []struct {
		name string
		open func(m *Model)
		want string
	}{
		{"form", func(m *Model) { press(m, text("e")...); press(m, keyClear) }, "› Alias          web█"},
		{"move prompt", func(m *Model) { press(m, keyPrefix, text("m")[0]); press(m, keyClear) }, "group: web█"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newModalModel(t)
			tt.open(m)
			m.Update(tea.PasteMsg{Content: "we\r\n\tb"})
			if !strings.Contains(screen(m), tt.want) {
				t.Fatalf("want %q on screen:\n%s", tt.want, screen(m))
			}
		})
	}
}

func TestPasteIsIgnoredByConfirmAndHelp(t *testing.T) {
	for _, tt := range modals[2:] {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newModalModel(t)
			tt.open(m)
			before := screen(m)
			m.Update(tea.PasteMsg{Content: "y"})
			if got := screen(m); got != before {
				t.Fatalf("paste changed the screen\n--- before ---\n%s\n--- after ---\n%s", before, got)
			}
		})
	}
}

func TestAnyKeyClosesConfirmAndHelp(t *testing.T) {
	for _, tt := range modals[2:] {
		t.Run(tt.name, func(t *testing.T) {
			m, f := newModalModel(t)
			before := screen(m)
			tt.open(m)
			press(m, keyPrefix)
			if got := screen(m); got != before {
				t.Fatalf("^B should close the %s\n--- want ---\n%s\n--- got ---\n%s", tt.name, before, got)
			}
			if len(f.deletes) != 0 {
				t.Fatalf("deletes = %v", f.deletes)
			}
		})
	}
}

func TestCtrlUClearsTheFilter(t *testing.T) {
	m := newTestModel(t, 80, 10)
	press(m, text("/prod")...)
	press(m, keyClear)
	if f := m.st.Filter(); f != "" {
		t.Fatalf("filter = %q, want empty", f)
	}
	if !m.filtering {
		t.Fatal("ctrl+u should keep the filter open")
	}
}

func TestPasteGoesToTheFilterBeingTyped(t *testing.T) {
	m := newTestModel(t, 80, 10)
	press(m, text("/")...)
	m.Update(tea.PasteMsg{Content: "prod\n"})
	if f := m.st.Filter(); f != "prod" {
		t.Fatalf("filter = %q, want prod", f)
	}
}

func TestPasteBehindTheMovePromptNeverReachesTheSession(t *testing.T) {
	// od prints the first byte the Session receives.
	m, _ := newSessionModel(t, script{sh: `stty raw -echo; head -c 1 | od -An -c`})
	press(m, keyEnter)
	time.Sleep(200 * time.Millisecond) // let stty run before sending
	press(m, keyPrefix, text("m")[0])
	m.Update(tea.PasteMsg{Content: "Z"})
	press(m, keyEsc, text("k")[0])
	pump(t, m, func() bool { return strings.Contains(screen(m), "   k") })
	if strings.Contains(screen(m), "   Z") {
		t.Fatalf("the paste reached the Session:\n%s", screen(m))
	}
}
