package workspace

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var (
	keyUp     = tea.KeyPressMsg{Code: tea.KeyUp}
	keyPgDown = tea.KeyPressMsg{Code: tea.KeyPgDown}
	keyPgUp   = tea.KeyPressMsg{Code: tea.KeyPgUp}
)

// helpScreenLines renders m and returns its lines without styling.
func helpScreenLines(m *Model) []string { return strings.Split(ansi.Strip(m.render()), "\n") }

// helpMore is the indicator the help screen shows when lines are below.
const helpMore = "more below"

// TestHelpListsEveryBinding checks that every declared binding has a line on
// the help screen showing its keys and what it does: all on one screen at a
// common 80x24 terminal, and reachable by scrolling on a short one.
func TestHelpListsEveryBinding(t *testing.T) {
	for _, h := range []int{24, 8} {
		m := newTestModel(t, 80, h)
		press(m, keyPrefix, text("?")[0])
		seen := helpScreenLines(m)
		for range len(bindings) {
			if !strings.Contains(strings.Join(helpScreenLines(m), "\n"), helpMore) {
				break
			}
			press(m, keyDown)
			seen = append(seen, helpScreenLines(m)...)
		}
		for _, b := range bindings {
			label := m.bindingLabel(b)
			found := false
			for _, line := range seen {
				if strings.Contains(line, label) && strings.Contains(line, b.help) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("height %d: help has no line with %q and %q", h, label, b.help)
			}
		}
	}
}

func TestHelpScrollsAndShowsWhenMoreIsBelow(t *testing.T) {
	m := newTestModel(t, 80, 8)
	press(m, keyPrefix, text("?")[0])
	first := helpScreenLines(m)
	if !strings.Contains(strings.Join(first, "\n"), helpMore) {
		t.Fatalf("a short help screen does not say more is below:\n%s", strings.Join(first, "\n"))
	}
	press(m, keyDown)
	if got := helpScreenLines(m); got[1] != first[2] {
		t.Errorf("after down, line 1 = %q, want %q", got[1], first[2])
	}
	press(m, keyUp)
	if got := helpScreenLines(m); got[1] != first[1] {
		t.Errorf("after up, line 1 = %q, want %q", got[1], first[1])
	}
	for range len(bindings) {
		press(m, keyPgDown)
	}
	last := helpScreenLines(m)
	if strings.Contains(strings.Join(last, "\n"), helpMore) {
		t.Errorf("at the end, help still says more is below:\n%s", strings.Join(last, "\n"))
	}
	if !strings.Contains(strings.Join(last, "\n"), bindings[len(bindings)-1].help) {
		t.Errorf("at the end, help lacks the last binding:\n%s", strings.Join(last, "\n"))
	}
	for range len(bindings) {
		press(m, keyPgUp)
	}
	if got := helpScreenLines(m); got[1] != first[1] {
		t.Errorf("after PgUp to the top, line 1 = %q, want %q", got[1], first[1])
	}
	if _, ok := m.modal.(*helpScreen); !ok {
		t.Fatal("scrolling closed the help screen")
	}
	press(m, text("x")...)
	if m.modal != nil {
		t.Error("another key did not close the help screen")
	}
}

// TestBindingsDoNotOverlap checks that no key is declared twice in the same context.
func TestBindingsDoNotOverlap(t *testing.T) {
	for _, ctx := range []keyContext{inAny, inList, inPrefix, inEnded} {
		seen := map[string]string{}
		for _, b := range bindings {
			for _, bk := range b.keys[ctx] {
				if prev, ok := seen[bk.key]; ok {
					t.Errorf("key %q in context %d is bound to both %q and %q", bk.key, ctx, prev, b.help)
				}
				seen[bk.key] = b.help
			}
		}
	}
}

func TestPrefixRefreshReloadsTheList(t *testing.T) {
	f := &fakeCatalog{entries: []Entry{{Alias: "alpha"}}, identity: "/k"}
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	f.entries = []Entry{{Alias: "alpha"}, {Alias: "beta"}}
	press(m, keyPrefix, text("r")[0])
	if m.st.Get("beta") == nil || m.status != "connection list refreshed" {
		t.Fatalf("after ^B r: beta = %v, status = %q", m.st.Get("beta"), m.status)
	}
	f.loadErr = errors.New("boom")
	press(m, keyPrefix, text("r")[0])
	if !m.statusErr || m.status != "reload: boom" {
		t.Fatalf("failed reload status = %q", m.status)
	}
}

func TestKeysDispatchThroughBindings(t *testing.T) {
	m := newTestModel(t, 80, 10)
	press(m, text("/")...)
	if !m.filtering {
		t.Fatal("/ in the list should start filtering")
	}
	press(m, keyEnter)
	press(m, keyPrefix, text("x")[0])
	if !m.statusErr || !strings.Contains(m.status, "unknown command ^B x") {
		t.Fatalf("unknown prefix command status = %q", m.status)
	}
	press(m, keyPrefix, text("b")[0])
	if !m.hideList {
		t.Fatal("^B b should hide the list")
	}
}
