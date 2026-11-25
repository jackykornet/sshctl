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
	if _, ok := m.mode.modal.(*helpScreen); !ok {
		t.Fatal("scrolling closed the help screen")
	}
	press(m, text("x")...)
	if m.mode.modal != nil {
		t.Error("another key did not close the help screen")
	}
}

// TestBindingsDoNotOverlap checks that no key is declared twice in the same context.
func TestBindingsDoNotOverlap(t *testing.T) {
	for _, ctx := range []keyContext{inList, inPrefix, inEnded, inTunnels} {
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

// TestKeyMeansTheSameInEveryContext checks ADR-0007's third rule: a key
// bound in more than one context runs the same binding in each.
func TestKeyMeansTheSameInEveryContext(t *testing.T) {
	owner := map[string]string{}
	for _, b := range bindings {
		for ctx := range numContexts {
			for _, bk := range b.keys[ctx] {
				if prev, ok := owner[bk.key]; ok && prev != b.help {
					t.Errorf("key %q means both %q and %q", bk.key, prev, b.help)
				}
				owner[bk.key] = b.help
			}
		}
	}
}

// TestListActionsAlsoWorkAfterThePrefix checks ADR-0007's first rule: a
// binding with keys in the list has the same keys after the prefix, unless
// it only moves around the list.
func TestListActionsAlsoWorkAfterThePrefix(t *testing.T) {
	for _, b := range bindings {
		if len(b.keys[inList]) == 0 || b.listOnly {
			continue
		}
		var list, prefix []string
		for _, bk := range b.keys[inList] {
			list = append(list, bk.key)
		}
		for _, bk := range b.keys[inPrefix] {
			prefix = append(prefix, bk.key)
		}
		if strings.Join(list, " ") != strings.Join(prefix, " ") {
			t.Errorf("%q: list keys %v, prefix keys %v", b.help, list, prefix)
		}
	}
}

func TestPaneSendsUnprefixedKeysToTheSession(t *testing.T) {
	m, _, in := paneModel(t, Options{}, "$ ")
	c := m.st.Active()
	press(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModAlt})
	if m.st.Active() != c {
		t.Fatalf("Alt+↓ in the pane made %s active", m.st.Active().Alias)
	}
	in.waitFor(t, "\x1b\x1b[D")
}

func TestBracesCycleTheActiveConnection(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab})
	m.st.SetCursor("staging")
	press(m, keyEnter) // staging and db-main open, staging active
	press(m, keyPrefix, text("}")[0])
	if a := m.st.Active().Alias; a == "staging" {
		t.Fatal("prefix } left staging active")
	}
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}) // to the list
	press(m, text("{")...)
	if a := m.st.Active().Alias; a != "staging" {
		t.Fatalf("{ in the list made %s active, want staging", a)
	}
}

func TestPrefixOpensThePickerAndTheFilterFromThePane(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	press(m, keyPrefix, text("P")[0])
	if m.picker() == nil {
		t.Fatal("prefix P did not open the Space picker")
	}
	press(m, keyEsc, keyPrefix, text("/")[0])
	if !m.filtering() || m.focus != focusList {
		t.Fatalf("prefix / in the pane: filtering %v, focus %v", m.filtering(), m.focus)
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
	press(m, keyPrefix, text("R")[0])
	if m.st.Get("beta") == nil || m.status != "connection list refreshed" {
		t.Fatalf("after ^B R: beta = %v, status = %q", m.st.Get("beta"), m.status)
	}
	f.loadErr = errors.New("boom")
	press(m, keyPrefix, text("R")[0])
	if !m.statusErr || m.status != "reload: boom" {
		t.Fatalf("failed reload status = %q", m.status)
	}
}

func TestKeysDispatchThroughBindings(t *testing.T) {
	m := newTestModel(t, 80, 10)
	press(m, text("/")...)
	if !m.filtering() {
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

func TestBracketsSelectTheNeighbouringTab(t *testing.T) {
	m := newTestModel(t, 80, 10)
	m.st.SetCursor("db-main")
	press(m, keyEnter)
	press(m, keyPrefix, text("n")[0], keyPrefix, text("n")[0]) // tabs 1-3, on 3
	c := m.st.Get("db-main")
	press(m, keyPrefix, text("[")[0])
	if c.Current != 1 {
		t.Fatalf("prefix [ in the pane: tab %d, want 2", c.Current+1)
	}
	press(m, keyPrefix, text("]")[0])
	if c.Current != 2 {
		t.Fatalf("prefix ] in the pane: tab %d, want 3", c.Current+1)
	}
	if m.scrolling() {
		t.Fatal("prefix [ should no longer scroll back")
	}
	press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}) // to the list
	press(m, text("[")...)
	press(m, text("[")...)
	if c.Current != 0 {
		t.Fatalf("[ [ in the list: tab %d, want 1", c.Current+1)
	}
	press(m, text("]")...)
	if c.Current != 1 || m.focus != focusList {
		t.Fatalf("] in the list: tab %d, focus %v; want tab 2 with the list kept", c.Current+1, m.focus)
	}
}
