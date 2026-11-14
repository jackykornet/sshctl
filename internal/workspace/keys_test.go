package workspace

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestHelpListsEveryBinding checks that the help screen, at a common 80×24
// terminal size, has a line for every declared binding showing its keys and
// what it does.
func TestHelpListsEveryBinding(t *testing.T) {
	m := newTestModel(t, 80, 24)
	press(m, keyPrefix, text("?")[0])
	screen := strings.Split(ansi.Strip(m.render()), "\n")
	for _, b := range bindings {
		label := m.bindingLabel(b)
		found := false
		for _, line := range screen {
			if strings.Contains(line, label) && strings.Contains(line, b.help) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("help has no line with %q and %q", label, b.help)
		}
	}
}

// TestBindingsDoNotOverlap checks that no key is declared twice in the same context.
func TestBindingsDoNotOverlap(t *testing.T) {
	for _, ctx := range []keyContext{inAny, inList, inPrefix, inEnded} {
		seen := map[string]string{}
		for _, b := range bindings {
			for _, k := range b.keys[ctx] {
				if prev, ok := seen[k]; ok {
					t.Errorf("key %q in context %d is bound to both %q and %q", k, ctx, prev, b.help)
				}
				seen[k] = b.help
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
