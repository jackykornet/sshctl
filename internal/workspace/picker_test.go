package workspace

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestPOpensThePickerAndEnterSwitchesToAnySpace(t *testing.T) {
	f := spaceCatalog()
	for i := 4; i <= 11; i++ {
		f.spaces = append(f.spaces, Space{Name: fmt.Sprintf("s%d", i)})
	}
	f.entries = append(f.entries, Entry{Alias: "far", Space: "s11"})
	m := newStoreModel(t, f)

	press(m, text("P")...)
	if m.picker() == nil {
		t.Fatal("P in the list did not open the Space picker")
	}
	for range 10 {
		press(m, keyDown)
	}
	press(m, keyEnter)
	if m.picker() != nil {
		t.Error("Enter did not close the picker")
	}
	if got := m.st.Space(); got != "s11" {
		t.Fatalf("after Enter on the 11th Space, active Space = %q, want s11", got)
	}
	if got := aliases(m.st.Rows()); !reflect.DeepEqual(got, []string{"far"}) {
		t.Errorf("s11 lists %v, want [far]", got)
	}
	if want := []string{"s11"}; !reflect.DeepEqual(f.activeSaves, want) {
		t.Errorf("saved active Spaces %q, want %q", f.activeSaves, want)
	}
}

// A cursor past the last row (the rows shrank under it) names no Space, so
// Enter neither switches nor reports a Space 0.
func TestEnterPastTheLastPickerRowDoesNothing(t *testing.T) {
	f := spaceCatalog()
	m := newStoreModel(t, f)
	press(m, text("P")...)
	p := m.picker()
	p.cursor = 99
	press(m, keyEnter)
	if m.mode.modal != p {
		t.Fatalf("Enter past the last row: modal %T, want the picker still open", m.mode.modal)
	}
	if status, _ := p.status(); strings.Contains(status, "Space 0") {
		t.Errorf("Enter past the last row said %q", status)
	}
	if len(f.activeSaves) != 0 {
		t.Errorf("Enter past the last row saved active Spaces %q", f.activeSaves)
	}
}

func TestThePickerOpensOnTheActiveSpaceAndEscClosesIt(t *testing.T) {
	f := spaceCatalog()
	f.activeSpace = "globex"
	m := newStoreModel(t, f)
	press(m, text("P")...)
	if p := m.picker(); p == nil || p.cursor != 2 {
		t.Fatalf("picker = %+v, want its cursor on globex (index 2)", p)
	}
	press(m, keyEsc)
	if m.mode.modal != nil {
		t.Error("Esc did not close the picker")
	}
	if len(f.activeSaves) != 0 {
		t.Errorf("closing the picker saved active Spaces %q", f.activeSaves)
	}
}

func TestThePickerAddsAndRenamesSpaces(t *testing.T) {
	f := spaceCatalog()
	f.activeSpace = "acme"
	m := newStoreModel(t, f)
	press(m, text("P")...)

	// On acme's row, which is in use, a takes the lowest free number.
	press(m, text("a")...)
	press(m, text("initech")...)
	press(m, keyEnter)
	p := m.picker()
	if p == nil || m.mode.modal != p {
		t.Fatalf("after adding, modal = %T, want the picker back", m.mode.modal)
	}
	if tab, _ := p.target(m); tab.Name != "initech" || tab.Number != 4 || p.cursor != 3 {
		t.Fatalf("cursor %d on %+v; want initech added as 4 with the cursor on it", p.cursor, tab)
	}
	if got := m.st.Space(); got != "acme" {
		t.Errorf("adding a Space switched to %q; only Enter switches", got)
	}

	// On the free row 7, a adds a Space with that number.
	p.cursor = 6
	press(m, text("a")...)
	press(m, text("hooli")...)
	press(m, keyEnter)
	if tab, _ := p.target(m); tab.Name != "hooli" || tab.Number != 7 || p.cursor != 6 {
		t.Fatalf("cursor %d on %+v; want hooli added as 7 under the cursor", p.cursor, tab)
	}

	// Rename the active Space acme: it stays active and keeps its list.
	p.cursor = 1
	press(m, text("e")...)
	press(m, keyClear)
	press(m, text("acme-corp")...)
	press(m, keyEnter)
	if got := m.st.Space(); got != "acme-corp" {
		t.Errorf("after renaming the active Space, active Space = %q, want acme-corp", got)
	}
	if got := aliases(m.st.Rows()); !reflect.DeepEqual(got, []string{"db"}) {
		t.Errorf("acme-corp lists %v, want [db]", got)
	}
	if want := []string{"add initech", "add hooli at 7", "rename acme→acme-corp"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Errorf("Space calls = %v, want %v", f.spaceOps, want)
	}
}

func TestThePickerOnlyAddsOnAFreeRow(t *testing.T) {
	f := spaceCatalog() // 1 default, 2 acme, 3 globex
	m := newStoreModel(t, f)
	press(m, text("P")...)
	p := m.picker()
	p.cursor = 4 // 5, free
	for _, k := range []string{"e", "d", "J", "K"} {
		press(m, text(k)...)
		if m.mode.modal != p {
			t.Fatalf("%s on a free row opened %T", k, m.mode.modal)
		}
	}
	press(m, keyEnter)
	if m.mode.modal != p || m.st.Space() != "" {
		t.Errorf("Enter on a free row: modal %T, active %q; want the picker open, nothing switched", m.mode.modal, m.st.Space())
	}
	if status, _ := p.status(); !strings.Contains(status, "no Space 5") {
		t.Errorf("Enter on a free row says %q, want no Space 5", status)
	}
	if len(f.spaceOps) != 0 {
		t.Errorf("keys on a free row called %v", f.spaceOps)
	}
}

func TestARefusedSpaceNameKeepsThePromptOpenWithTheReason(t *testing.T) {
	f := spaceCatalog()
	m := newStoreModel(t, f)
	press(m, text("P")...)
	press(m, text("a")...)
	press(m, text("42")...)
	f.spaceErr = errors.New(`"42": a space name cannot be only digits`)
	press(m, keyEnter)
	in, ok := m.mode.modal.(*spaceInput)
	if !ok || !strings.Contains(in.err, "only digits") {
		t.Fatalf("modal = %#v, want the prompt open with the reason", m.mode.modal)
	}
	press(m, keyEsc)
	if m.mode.modal != m.picker() || m.picker() == nil {
		t.Errorf("Esc in the prompt: modal = %T, want the picker", m.mode.modal)
	}
}

func TestThePickerDeletesOnlyAnEmptySpaceOtherThanTheDefault(t *testing.T) {
	f := spaceCatalog() // default (web, mine; Group prod), acme (db; Group data), globex (empty)
	f.activeSpace = "globex"
	m := newStoreModel(t, f)
	press(m, text("P")...)
	p := m.picker()

	tests := []struct {
		cursor int
		want   string
	}{
		{0, "the default space cannot be deleted"},
		{1, `"acme" still has groups data and connections db`},
	}
	for _, tt := range tests {
		p.cursor = tt.cursor
		f.spaceErr = errors.New(tt.want) // the catalog refuses, naming what is left
		press(m, text("d")...)
		if status, _ := m.mode.modal.status(); m.mode.modal != p || !strings.Contains(status, tt.want) {
			t.Errorf("d on Space %d: modal %T, status %q; want the picker saying %q without asking", tt.cursor+1, m.mode.modal, status, tt.want)
		}
	}
	if want := []string{"delete default", "delete acme"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Fatalf("Space calls = %v, want %v", f.spaceOps, want)
	}
	f.spaceOps = nil

	p.cursor = 2
	press(m, text("d")...)
	if c := m.confirm(); c == nil || !strings.Contains(c.question, "Delete space globex?") {
		t.Fatalf("d on the empty globex: modal %#v, want it to ask", m.mode.modal)
	}
	press(m, text("n")...)
	if m.mode.modal != p || len(f.spaceOps) != 0 {
		t.Fatalf("n: modal %T, Space calls %v; want the picker back and nothing deleted", m.mode.modal, f.spaceOps)
	}
	press(m, text("d")...)
	press(m, text("y")...)
	if want := []string{"delete globex"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Fatalf("Space calls = %v, want %v", f.spaceOps, want)
	}
	if got := len(m.st.SpaceTabs()); got != 2 || m.mode.modal != p || p.cursor != 2 {
		t.Errorf("after deleting: %d Spaces, modal %T, cursor %d; want 2, the picker, cursor still on row 3", got, m.mode.modal, p.cursor)
	}
	if tab, _ := p.target(m); !tab.Free() || tab.Number != 3 {
		t.Errorf("row 3 after deleting globex = %+v, want it free", tab)
	}
	if got := m.st.Space(); got != "" {
		t.Errorf("deleting the active Space left %q active, want the default Space", got)
	}
}

func TestShiftArrowsMoveASpaceToTheNextNumber(t *testing.T) {
	f := spaceCatalog() // 1 default, 2 acme, 3 globex
	m := newStoreModel(t, f)
	press(m, text("P")...)
	p := m.picker()
	p.cursor = 2
	press(m, keyShiftDown) // into the free 4
	if tab, _ := p.target(m); tab.Name != "globex" || tab.Number != 4 || p.cursor != 3 {
		t.Fatalf("cursor %d on %+v; want globex moved to 4 under the cursor", p.cursor, tab)
	}
	if tab := m.st.SpaceSlots()[2]; !tab.Free() {
		t.Errorf("3 after moving globex away = %+v, want it free", tab)
	}
	press(m, text("K")...) // back into the free 3
	press(m, keyShiftUp)   // into acme's 2: they swap
	if names := slotNames(m); !reflect.DeepEqual(names, []string{"default", "globex", "acme"}) || p.cursor != 1 {
		t.Errorf("Spaces 1-3 %v, cursor %d; want globex and acme swapped, the cursor following globex", names, p.cursor)
	}
	p.cursor = 0
	press(m, keyShiftUp) // 1: nothing above it
	p.cursor = 8
	press(m, text("J")...) // 9 is free: nothing to move
	if want := []string{"move globex 4", "move globex 3", "move globex 2"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Errorf("Space calls = %v, want %v", f.spaceOps, want)
	}
}

// slotNames is the names of Spaces 1-3, "" for a free number.
func slotNames(m *Model) []string {
	var names []string
	for _, tab := range m.st.SpaceSlots()[:3] {
		names = append(names, tab.Name)
	}
	return names
}

func TestSpacePickerGolden(t *testing.T) {
	m := newSpaceModel(t, 80, 13)
	m.st.SetCursor("staging")
	press(m, keyEnter) // a live Session in the default Space
	press(m, keyPrefix, text("2")[0])
	press(m, text("P")...)
	press(m, keyDown)
	golden(t, "space-picker", m)
	// Every key hinted fits at 80 columns, C color before Esc close.
	hint := m.mode.modal.(hinter).hints()[0].String()
	if !strings.HasSuffix(hint, "C color · Esc close") {
		t.Errorf("picker hints = %q, want C color before Esc close at the end", hint)
	}
	if !strings.Contains(screen(m), hint) {
		t.Errorf("picker hints %q cut off at 80 columns:\n%s", hint, screen(m))
	}
}

func TestThePickerRenamesAndMovesTheDefaultSpace(t *testing.T) {
	f := spaceCatalog() // default (web, mine), acme, globex
	m := newStoreModel(t, f)
	press(m, text("P")...)
	press(m, text("e")...)
	press(m, keyClear)
	press(m, text("home")...)
	press(m, keyEnter)
	press(m, text("J")...)
	if want := []string{"rename default→home", "move home 2"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Fatalf("Space calls = %v, want %v", f.spaceOps, want)
	}
	tab := m.st.SpaceSlots()[1]
	if tab.Name != "home" || !tab.Active || m.picker().cursor != 1 {
		t.Errorf("Space 2 %+v, cursor %d; want home, still active, under the cursor", tab, m.picker().cursor)
	}
	if got := aliases(m.st.Rows()); !reflect.DeepEqual(got, []string{"web", "mine"}) {
		t.Errorf("home lists %v, want [web mine]", got)
	}
}
