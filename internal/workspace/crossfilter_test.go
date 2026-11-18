package workspace

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

func TestEachCrossSpaceMatchShowsItsSpace(t *testing.T) {
	m := newSpaceModel(t, 80, 10)
	press(m, text("/a")...) // staging, db's Group data
	golden(t, "filter-spaces", m)
	got := strings.Join(listText(m), "\n")
	for _, want := range []string{"staging [default/web]", "db [acme/data]"} {
		if !strings.Contains(got, want) {
			t.Errorf("list lacks %q:\n%s", want, got)
		}
	}
}

func TestGroupActionsRefuseAMatchFromAnotherSpace(t *testing.T) {
	m := newSpaceModel(t, 80, 10)
	press(m, text("/db")...)
	press(m, keyEnter)
	for _, k := range []string{"g", "m"} {
		press(m, keyPrefix, text(k)[0])
		if m.modal != nil || !strings.Contains(m.status, "db is in space acme") {
			t.Errorf("prefix %s on acme's db: modal %T, status %q; want it refused", k, m.modal, m.status)
		}
	}
}

func TestClickingAMatchFromAnotherSpaceSwitchesToIt(t *testing.T) {
	m := newSpaceModel(t, 80, 10)
	press(m, text("/a")...)
	press(m, keyEnter)
	m.Update(tea.MouseClickMsg{X: 3, Y: 3, Button: tea.MouseLeft}) // db, under the bar, filter and staging
	if got := m.st.Space(); got != "acme" {
		t.Fatalf("clicking db left Space %q active, want acme", got)
	}
	if m.drag != nil {
		t.Error("a click on a match from another Space started a drag")
	}
}
