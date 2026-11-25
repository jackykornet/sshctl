package workspace

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// hintSites opens each place that shows a hint line, and returns the lines
// it shows and the keys bound there.
var hintSites = []struct {
	name string
	open func(t *testing.T) (*Model, []hintLine, []boundKey)
}{
	{"status line, list focus", func(t *testing.T) (*Model, []hintLine, []boundKey) {
		m := newTestModel(t, 140, 10)
		return m, m.statusHints(), statusKeys(m)
	}},
	{"status line, pane focus", func(t *testing.T) (*Model, []hintLine, []boundKey) {
		m := newTestModel(t, 140, 10)
		m.st.SetCursor("db-main")
		press(m, keyEnter)
		return m, m.statusHints(), statusKeys(m)
	}},
	{"getting started", func(t *testing.T) (*Model, []hintLine, []boundKey) {
		m := newEmptyModel(t, 80, 24)
		add, spaces, help := m.gettingStartedHints()
		return m, []hintLine{add, spaces, help}, statusKeys(m)
	}},
	{"ended banner", func(t *testing.T) (*Model, []hintLine, []boundKey) {
		m, sess := newPaneModel(t, "$ ")
		endSession(m, sess, 1)
		return m, []hintLine{m.endedHints()}, m.contextKeys(inEnded)
	}},
	{"scroll mode", func(t *testing.T) (*Model, []hintLine, []boundKey) {
		m, _ := newPaneModel(t, strings.Repeat("line\n", 40))
		press(m, keyPrefix, text("s")[0])
		if !m.scrolling() {
			t.Fatal("prefix s did not scroll")
		}
		return m, []hintLine{m.scrollHints()}, m.scrollKeys()
	}},
	{"add form", modalSite(func(m *Model) { press(m, text("a")...) })},
	{"edit form", modalSite(func(m *Model) { press(m, text("e")...) })},
	{"form after a prefix command", modalSite(func(m *Model) { press(m, text("e")...); press(m, keyPrefix, text("n")[0]) })},
	{"move prompt", modalSite(func(m *Model) { press(m, keyPrefix, text("m")[0]) })},
	{"confirmation", modalSite(func(m *Model) { press(m, text("d")...) })},
	{"help screen", modalSite(func(m *Model) { press(m, keyPrefix, text("?")[0]) })},
	{"new group prompt", modalSite(func(m *Model) { m.st.SetCursorGroup("prod"); press(m, text("a")...) })},
	{"rename group prompt", modalSite(func(m *Model) { m.st.SetCursorGroup("prod"); press(m, text("e")...) })},
	{"space picker", modalSite(func(m *Model) { press(m, text("P")...) })},
	{"key picker", modalSite(func(m *Model) { press(m, text("I")...) })},
	{"history picker", modalSite(func(m *Model) { press(m, text("H")...) })},
	{"new space prompt", modalSite(func(m *Model) { press(m, text("P")...); press(m, text("a")...) })},
	{"group color prompt", modalSite(func(m *Model) { m.st.SetCursorGroup("prod"); press(m, text("C")...) })},
	{"space color prompt", modalSite(func(m *Model) { press(m, text("P")...); press(m, text("C")...) })},
}

// statusKeys is every key the status line may hint at: the list's bare keys
// while the list has focus, and the prefix commands, each as "^B key".
func statusKeys(m *Model) []boundKey {
	var out []boundKey
	if m.focus == focusList {
		out = m.contextKeys(inList)
	}
	for _, bk := range m.contextKeys(inPrefix) {
		bk.key = keyHint(m.opts.PrefixKey) + " " + bk.key
		out = append(out, bk)
	}
	return out
}

// modalSite opens a modal with open, in a Workspace with web under the cursor.
func modalSite(open func(m *Model)) func(t *testing.T) (*Model, []hintLine, []boundKey) {
	return func(t *testing.T) (*Model, []hintLine, []boundKey) {
		f := &fakeCatalog{entries: storeEntries()}
		m, err := New(Options{Catalog: f})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.closeAll)
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
		m.st.SetCursor("web")
		open(m)
		md, ok := m.mode.modal.(hinter)
		if !ok {
			t.Fatalf("modal %T shows no hints", m.mode.modal)
		}
		return m, md.hints(), md.keys()
	}
}

// TestHintsNameOnlyBoundKeys checks that every key a hint line names is
// bound where the line shows, to the action it names, and that the line
// is on the screen.
func TestHintsNameOnlyBoundKeys(t *testing.T) {
	for _, site := range hintSites {
		t.Run(site.name, func(t *testing.T) {
			m, lines, table := site.open(t)
			if len(lines) == 0 {
				t.Fatal("no hint lines")
			}
			for _, l := range lines {
				if len(l.items) == 0 {
					t.Error("an empty hint line")
				}
				for _, it := range l.items {
					for _, k := range it.keys {
						if !bound(table, it.lead+k, it.name) {
							t.Errorf("hint %q names %q, which is not bound to %q here", l, k, it.name)
						}
					}
				}
			}
			if s := screen(m); !strings.Contains(s, lines[0].String()) {
				t.Errorf("screen lacks %q:\n%s", lines[0], s)
			}
		})
	}
}

// While a modal is open, the status line hints only at the modal's keys:
// the list's, bound elsewhere, do not also show (ADR-0007).
func TestModalStatusLineHintsOnlyItsKeys(t *testing.T) {
	for _, site := range hintSites {
		t.Run(site.name, func(t *testing.T) {
			m, lines, _ := site.open(t)
			if m.mode.modal == nil {
				t.Skip("no modal")
			}
			rows := strings.Split(strings.TrimRight(screen(m), "\n"), "\n")
			status := rows[len(rows)-1]
			for _, l := range lines {
				status = strings.Replace(status, l.String(), "", 1)
			}
			// Every status hint line outside a modal ends with help.
			if help := m.hint(inPrefix, "?").String(); strings.Contains(status, help) {
				t.Errorf("status line hints at keys outside the modal: %q", status)
			}
		})
	}
}

func bound(table []boundKey, key, name string) bool {
	for _, bk := range table {
		if bk.key == key {
			return name != "" && bk.name == name
		}
	}
	return false
}

func TestHintLineFormat(t *testing.T) {
	table := []boundKey{
		on("enter", nil).as("switch"),
		on("shift+up", nil).as("move"),
		on("shift+down", nil).as("move"),
		on("esc", nil).as("close"),
	}
	got := hintsFor(table, "enter", "shift+up", "shift+down", "esc").String()
	if want := "Enter switch · Shift+↑/↓ move · Esc close"; got != want {
		t.Errorf("hint line = %q, want %q", got, want)
	}
	m := newTestModel(t, 100, 10)
	if got, want := m.hint(inPrefix, "?", "w").String(), "^B: ? help · w close"; got != want {
		t.Errorf("prefix hint line = %q, want %q", got, want)
	}
}
