package workspace

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestOpeningAMatchFromAnotherSpaceSwitchesToIt(t *testing.T) {
	m := newSpaceModel(t, 80, 10) // default: staging, prod-web (web); acme: db (data); idle
	press(m, text("/db")...)
	press(m, keyEnter) // done typing
	if got := rowNames(m.st.Rows()); !reflect.DeepEqual(got, []string{"db"}) {
		t.Fatalf("filter db lists %v, want acme's db", got)
	}
	press(m, keyEnter)
	if got := m.st.Space(); got != "acme" {
		t.Fatalf("opening db left Space %q active, want acme", got)
	}
	if c := m.st.Active(); c == nil || c.Alias != "db" || !c.Open() {
		t.Fatalf("Active connection = %v, want db with a tab", c)
	}

	// Clearing the filter shows the active Space's list again.
	m.focus = focusList
	press(m, keyEsc)
	if got, want := rowNames(m.st.Rows()), []string{"[data]", "db"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after Esc, rows = %v, want acme's %v", got, want)
	}
}

// newPathModel is newSpaceModelWide with a description on db and an
// ungrouped cache beside it in acme.
func newPathModel(t *testing.T, w, h, listWidth int) *Model {
	t.Helper()
	m, err := New(Options{ListWidth: listWidth, Catalog: &fakeCatalog{
		entries: []Entry{
			{Alias: "staging", Group: "web"},
			{Alias: "prod-web", Description: "public site", Group: "web"},
			{Alias: "db", Description: "postgres", Space: "acme", Group: "data"},
			{Alias: "cache", Space: "acme"},
		},
		groups:      []string{"web"},
		spaces:      []Space{{Name: "acme"}, {Name: "idle", Number: 5}},
		spaceGroups: []Group{{Name: "data", Space: "acme"}},
		identity:    "/home/me/.ssh/id_ed25519",
	}})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// A match from another Space shows its path, Space › Group › alias, or
// Space › alias when ungrouped; one in the active Space keeps its row.
func TestEachCrossSpaceMatchShowsItsPath(t *testing.T) {
	m := newPathModel(t, 80, 10, 0)
	press(m, text("/a")...) // staging, db's Group data, cache
	golden(t, "filter-spaces", m)
	got := strings.Join(listText(m), "\n")
	for _, want := range []string{"○ staging [web]", "○ acme › data › db postgres", "○ acme › cache"} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "default ›") || strings.Contains(got, "[default") {
		t.Errorf("a match in the active Space names its Space:\n%s", got)
	}
}

// A narrow list cuts the path short and keeps the alias whole.
func TestANarrowListTruncatesThePathNotTheAlias(t *testing.T) {
	for _, width := range []int{12, 14, 16} {
		m := newPathModel(t, 80, 10, width)
		press(m, text("/db")...)
		var row string
		for _, l := range listText(m) {
			if strings.Contains(l, "○") {
				row = l
			}
		}
		if !strings.HasSuffix(row, "… db") {
			t.Errorf("list %d wide: row = %q, want a shortened path then db", width, row)
		}
		if w := ansi.StringWidth(row); w > width {
			t.Errorf("list %d wide: row %q is %d columns", width, row, w)
		}
	}
}

func TestEveryActionOnAMatchFromAnotherSpaceSwitchesToIt(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyPressMsg
		check func(t *testing.T, m *Model)
	}{
		{"prefix n opens a tab there", []tea.KeyPressMsg{keyPrefix, text("n")[0]}, func(t *testing.T, m *Model) {
			if c := m.st.Active(); c == nil || c.Alias != "db" {
				t.Errorf("Active connection = %v, want db", c)
			}
		}},
		{"prefix g opens its Group there", []tea.KeyPressMsg{keyPrefix, text("g")[0]}, func(t *testing.T, m *Model) {
			if c := m.st.Active(); c == nil || c.Alias != "db" {
				t.Errorf("Active connection = %v, want db of Group data", c)
			}
		}},
		{"prefix m moves it among its own Groups", []tea.KeyPressMsg{keyPrefix, text("m")[0]}, func(t *testing.T, m *Model) {
			if in, ok := m.mode.modal.(*moveInput); !ok || in.alias != "db" {
				t.Errorf("modal = %#v, want the move prompt for db", m.mode.modal)
			}
		}},
		{"a adds in its Space and Group", text("a"), func(t *testing.T, m *Model) {
			if f := m.form(); f == nil || f.space != "acme" || f.spec().Group != "data" {
				t.Errorf("form = %+v, want a new Connection in acme's data", f)
			}
		}},
		{"e edits it with its Space's Groups", text("e"), func(t *testing.T, m *Model) {
			if f := m.form(); f == nil || f.old != "db" || !reflect.DeepEqual(m.st.groups, []string{"data"}) {
				t.Errorf("form %+v with Groups %v, want db's form over acme's Groups", f, m.st.groups)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSpaceModel(t, 80, 10)
			press(m, text("/db")...)
			press(m, keyEnter)
			press(m, tt.keys...)
			if got := m.st.Space(); got != "acme" {
				t.Fatalf("active Space = %q, want acme", got)
			}
			tt.check(t, m)
		})
	}
}

func TestClickingAMatchFromAnotherSpaceSwitchesToIt(t *testing.T) {
	m := newSpaceModel(t, 80, 10)
	press(m, text("/a")...)
	press(m, keyEnter)
	m.Update(tea.MouseClickMsg{X: 3, Y: 4, Button: tea.MouseLeft}) // db, under the bar, its rule, filter and staging
	if got := m.st.Space(); got != "acme" {
		t.Fatalf("clicking db left Space %q active, want acme", got)
	}
	if m.mode.drag != nil {
		t.Error("a click on a match from another Space started a drag")
	}
}
