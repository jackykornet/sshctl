package workspace

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// spaceCatalog is a fakeCatalog with the default Space (web in Group prod,
// and mine) and Spaces acme (db in Group data) and globex (empty), showing
// the default Space.
func spaceCatalog() *fakeCatalog {
	return &fakeCatalog{
		entries:     append(storeEntries(), Entry{Alias: "db", Space: "acme", Group: "data"}),
		groups:      []string{"prod"},
		spaces:      []Space{{Name: "acme"}, {Name: "globex"}},
		spaceGroups: []Group{{Name: "data", Space: "acme"}},
	}
}

func TestDigitsSwitchAndSaveTheActiveSpace(t *testing.T) {
	f := spaceCatalog()
	m := newStoreModel(t, f)
	press(m, text("2")...)
	if got := m.st.Space(); got != "acme" {
		t.Fatalf("after 2 in the list, active Space = %q, want acme", got)
	}
	if got := aliases(m.st.Rows()); !reflect.DeepEqual(got, []string{"db"}) {
		t.Errorf("acme lists %v, want [db]", got)
	}
	m.focus = focusPane
	press(m, keyPrefix, text("1")[0])
	if got := m.st.Space(); got != "" {
		t.Fatalf("after prefix 1 in the pane, active Space = %q, want the default", got)
	}
	if m.focus != focusList {
		t.Error("a Space with no Active connection should give the list focus")
	}
	press(m, text("9")...)
	if got := m.st.Space(); got != "" || !m.statusErr || !strings.Contains(m.status, "no space 9") {
		t.Errorf("9 with 3 Spaces: Space %q, status %q; want it refused", got, m.status)
	}
	if want := []string{"acme", ""}; !reflect.DeepEqual(f.activeSaves, want) {
		t.Errorf("saved active Spaces %q, want %q", f.activeSaves, want)
	}
}

func TestTheWorkspaceOpensOnTheSavedActiveSpace(t *testing.T) {
	f := spaceCatalog()
	f.activeSpace = "acme"
	m := newStoreModel(t, f)
	if got := aliases(m.st.Rows()); !reflect.DeepEqual(got, []string{"db"}) {
		t.Errorf("rows = %v, want acme's [db]", got)
	}
}

func TestNewConnectionsAndGroupsGoIntoTheActiveSpace(t *testing.T) {
	f := spaceCatalog()
	f.activeSpace = "acme"
	m := newStoreModel(t, f)

	m.st.SetCursor("db")
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	press(m, keyCtrlS)
	if len(f.specs) != 1 || f.specs[0].Space != "acme" || f.specs[0].Group != "data" {
		t.Fatalf("saved %+v, want api in acme's data", f.specs)
	}
	if got := aliases(m.st.Rows()); !reflect.DeepEqual(got, []string{"api", "db"}) {
		t.Errorf("acme lists %v, want the new api with db", got)
	}

	m.st.SetCursorGroup("data")
	press(m, text("a")...)
	press(m, text("ops")...)
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m.st.SetCursorGroup("data")
	press(m, text("e")...)
	press(m, keyClear)
	press(m, text("stats")...)
	press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if want := []string{"add ops", "rename data→stats"}; !reflect.DeepEqual(f.groupOps, want) {
		t.Fatalf("group calls = %v, want %v", f.groupOps, want)
	}
	if want := []string{"acme", "acme"}; !reflect.DeepEqual(f.groupSpaces, want) {
		t.Errorf("group calls went to Spaces %q, want %q", f.groupSpaces, want)
	}
}

// newSpaceModel is newTestModel's Workspace with Spaces acme (db in Group
// data) and idle as well, w by h.
func newSpaceModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m, err := New(Options{Catalog: &fakeCatalog{
		entries: []Entry{
			{Alias: "staging", Group: "web"},
			{Alias: "prod-web", Description: "public site", Group: "web"},
			{Alias: "db", Space: "acme", Group: "data"},
		},
		groups:      []string{"web"},
		spaces:      []Space{{Name: "acme"}, {Name: "idle"}},
		spaceGroups: []Group{{Name: "data", Space: "acme"}},
		identity:    "/home/me/.ssh/id_ed25519",
	}})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func TestSpaceBarGolden(t *testing.T) {
	m := newSpaceModel(t, 80, 8)
	m.st.SetCursor("staging")
	press(m, keyEnter)                // a live Session in the default Space
	press(m, keyPrefix, text("2")[0]) // the pane has focus
	golden(t, "spaces", m)

	m.st.SetCursor("db")
	press(m, keyEnter)
	golden(t, "spaces-open", m)
}

func TestSpaceBarStylesActiveLiveAndIdleSpaces(t *testing.T) {
	m := newSpaceModel(t, 80, 8)
	m.st.SetCursor("staging")
	press(m, keyEnter)
	press(m, keyPrefix, text("2")[0])
	want := " [1]" + " " + spaceActiveStyle.Render("[2]") + " " + dimStyle.Render("[3]")
	if got := m.spaceBar(); !strings.HasPrefix(got, want) {
		t.Errorf("space bar = %q, want it to start %q", got, want)
	}
}

func TestReloadOpensTheSavedSpaceWhenTheActiveOneIsGone(t *testing.T) {
	f := spaceCatalog()
	f.activeSpace = "acme"
	m := newStoreModel(t, f)
	// Another terminal renames acme to acme-corp.
	f.spaces[0].Name = "acme-corp"
	f.spaceGroups[0].Space = "acme-corp"
	f.entries[len(f.entries)-1].Space = "acme-corp"
	f.activeSpace = "acme-corp"
	press(m, keyPrefix, text("r")[0])
	if got := m.st.Space(); got != "acme-corp" {
		t.Errorf("after the reload, active Space = %q, want acme-corp", got)
	}
}
