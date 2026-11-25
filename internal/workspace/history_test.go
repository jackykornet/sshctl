package workspace

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// historyCatalog has Connections in two Spaces, an Orphaned connection and a
// History naming them all, plus an alias with no record at all.
func historyCatalog() *fakeCatalog {
	return &fakeCatalog{
		entries: []Entry{
			{Alias: "web", Description: "public site", Group: "prod"},
			{Alias: "mine", Description: "hand written"},
			{Alias: "db", Description: "postgres", Space: "acme", Group: "data"},
			{Alias: "old-box", Description: "retired", Orphaned: true},
		},
		groups:      []string{"prod"},
		spaces:      []Space{{Name: "acme"}},
		spaceGroups: []Group{{Name: "data", Space: "acme"}},
		history:     []string{"db", "old-box", "web", "gone", "mine"},
	}
}

// historyRows is the aliases the open History picker lists, in order.
func historyRows(t *testing.T, m *Model) []string {
	t.Helper()
	p := m.history()
	if p == nil {
		t.Fatal("the History picker is not open")
	}
	var out []string
	for _, r := range p.rows(m) {
		out = append(out, r.alias)
	}
	return out
}

func TestHAndPrefixHOpenTheHistoryPicker(t *testing.T) {
	m, _ := newRecordingCatalogModel(t, historyCatalog())
	press(m, text("H")...)
	if got, want := historyRows(t, m), []string{"db", "old-box", "web", "gone", "mine"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("H lists %v, want History most recent first %v", got, want)
	}
	press(m, keyEsc)
	if m.history() != nil {
		t.Fatal("Esc did not close the History picker")
	}

	m.st.SetCursor("web")
	press(m, keyEnter) // the pane has focus now
	press(m, keyPrefix, text("H")[0])
	if m.history() == nil {
		t.Fatal("prefix H in the pane did not open the History picker")
	}
}

func TestLowercaseHStillCollapsesAGroup(t *testing.T) {
	m, _ := newRecordingCatalogModel(t, historyCatalog())
	m.st.SetCursorGroup("prod")
	press(m, text("h")...)
	if m.history() != nil || !m.st.Collapsed("prod") {
		t.Errorf("h on a group header: picker %v, collapsed %v; want the group collapsed", m.history() != nil, m.st.Collapsed("prod"))
	}
}

func TestTypingFiltersHistoryByAliasDescriptionGroupOrSpace(t *testing.T) {
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"WEB", []string{"web"}},                        // alias, any case
		{"hand", []string{"mine"}},                      // description
		{"data", []string{"db"}},                        // Group
		{"acme", []string{"db"}},                        // Space
		{"default", []string{"old-box", "web", "mine"}}, // the default Space's name
		{"gon", []string{"gone"}},                       // an alias with no Connection
	} {
		t.Run(tc.filter, func(t *testing.T) {
			m, _ := newRecordingCatalogModel(t, historyCatalog())
			press(m, text("H")...)
			press(m, text(tc.filter)...)
			if got := historyRows(t, m); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("filter %q lists %v, want %v", tc.filter, got, tc.want)
			}
		})
	}
}

func TestEnterInHistoryOpensAClosedConnection(t *testing.T) {
	m, r := newRecordingCatalogModel(t, historyCatalog())
	press(m, text("H")...)
	press(m, text("mine")...)
	press(m, keyEnter)
	if m.history() != nil {
		t.Error("Enter did not close the History picker")
	}
	if got := r.started(); !reflect.DeepEqual(got, []string{"mine"}) {
		t.Fatalf("Sessions started %v, want [mine]", got)
	}
	if a := m.st.Active(); a == nil || a.Alias != "mine" || m.focus != focusPane {
		t.Errorf("after Enter: active %v, focus %v; want mine with the pane focused", a, m.focus)
	}
}

func TestEnterInHistorySwitchesToAnOpenConnection(t *testing.T) {
	m, r := newRecordingCatalogModel(t, historyCatalog())
	m.st.SetCursor("web")
	press(m, keyEnter)
	m.focus = focusList
	m.st.SetCursor("mine")
	press(m, keyEnter)
	press(m, text("H")...) // the pane has focus, so H is the Session's
	if m.history() != nil {
		t.Fatal("H in the pane went to the picker, not the Session")
	}
	press(m, keyPrefix, text("H")[0])
	press(m, text("web")...)
	press(m, keyEnter)
	if got := r.started(); !reflect.DeepEqual(got, []string{"web", "mine"}) {
		t.Fatalf("Sessions started %v, want [web mine]: Enter on open web started another", got)
	}
	if a := m.st.Active(); a == nil || a.Alias != "web" {
		t.Errorf("active %v, want web", a)
	}
}

func TestEnterInHistoryOnAnotherSpaceMakesItActive(t *testing.T) {
	m, r := newRecordingCatalogModel(t, historyCatalog())
	press(m, text("H")...)
	press(m, keyEnter) // db, in acme
	if got := r.started(); !reflect.DeepEqual(got, []string{"db"}) {
		t.Fatalf("Sessions started %v, want [db]", got)
	}
	if got := m.st.Space(); got != "acme" {
		t.Errorf("active Space %q, want acme", got)
	}
	if want := []string{"acme"}; !reflect.DeepEqual(r.activeSaves, want) {
		t.Errorf("saved active Spaces %q, want %q", r.activeSaves, want)
	}
	if a := m.st.Active(); a == nil || a.Alias != "db" {
		t.Errorf("active %v, want db", a)
	}
}

func TestEnterOnAMissingHistoryEntryOpensNothing(t *testing.T) {
	for _, alias := range []string{"old-box", "gone"} {
		t.Run(alias, func(t *testing.T) {
			m, r := newRecordingCatalogModel(t, historyCatalog())
			press(m, text("H")...)
			press(m, text(alias)...)
			press(m, keyEnter)
			if got := r.started(); len(got) != 0 {
				t.Fatalf("Enter on missing %s started %v", alias, got)
			}
			p := m.history()
			if p == nil {
				t.Fatal("Enter on a missing entry closed the picker")
			}
			if status, _ := p.status(); !strings.Contains(status, alias+" is missing from ssh_config") {
				t.Errorf("status %q does not say why %s did not open", status, alias)
			}
		})
	}
}

func TestHistoryPickerGolden(t *testing.T) {
	m, _ := newRecordingCatalogModel(t, historyCatalog())
	press(m, text("H")...)
	press(m, keyDown)
	golden(t, "history-picker", m)
}

func TestHistoryPickerShowsSpaceDescriptionAndMissing(t *testing.T) {
	m, _ := newRecordingCatalogModel(t, historyCatalog())
	press(m, text("H")...)
	screen := ansi.Strip(m.render())
	for _, want := range []string{"db", "acme", "postgres", "old-box", "(missing)", "gone", "web", "public site"} {
		if !strings.Contains(screen, want) {
			t.Errorf("History picker does not show %q:\n%s", want, screen)
		}
	}
}
