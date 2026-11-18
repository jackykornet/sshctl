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

func TestThePickerOpensOnTheActiveSpaceAndEscClosesIt(t *testing.T) {
	f := spaceCatalog()
	f.activeSpace = "globex"
	m := newStoreModel(t, f)
	press(m, text("P")...)
	if p := m.picker(); p == nil || p.cursor != 2 {
		t.Fatalf("picker = %+v, want its cursor on globex (index 2)", p)
	}
	press(m, keyEsc)
	if m.modal != nil {
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

	press(m, text("a")...)
	press(m, text("initech")...)
	press(m, keyEnter)
	p := m.picker()
	if p == nil || m.modal != p {
		t.Fatalf("after adding, modal = %T, want the picker back", m.modal)
	}
	if got := m.st.SpaceTabs(); len(got) != 4 || got[3].Name != "initech" || p.cursor != 3 {
		t.Fatalf("Spaces %+v, cursor %d; want initech added fourth with the cursor on it", got, p.cursor)
	}
	if got := m.st.Space(); got != "acme" {
		t.Errorf("adding a Space switched to %q; only Enter switches", got)
	}

	// Rename the active Space acme: it stays active and keeps its list.
	press(m, keyUp, keyUp)
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
	if want := []string{"add initech", "rename acme→acme-corp"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Errorf("Space calls = %v, want %v", f.spaceOps, want)
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
	in, ok := m.modal.(*spaceInput)
	if !ok || !strings.Contains(in.err, "only digits") {
		t.Fatalf("modal = %#v, want the prompt open with the reason", m.modal)
	}
	press(m, keyEsc)
	if m.modal != m.picker() || m.picker() == nil {
		t.Errorf("Esc in the prompt: modal = %T, want the picker", m.modal)
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
		{1, "cannot delete space acme: it still has groups data and connections db"},
	}
	for _, tt := range tests {
		p.cursor = tt.cursor
		press(m, text("d")...)
		if status, _ := m.modal.status(); m.modal != p || !strings.Contains(status, tt.want) {
			t.Errorf("d on Space %d: modal %T, status %q; want the picker saying %q", tt.cursor+1, m.modal, status, tt.want)
		}
	}
	if len(f.spaceOps) != 0 {
		t.Fatalf("refused deletes called the catalog: %v", f.spaceOps)
	}

	p.cursor = 2
	press(m, text("d")...)
	if c := m.confirm(); c == nil || !strings.Contains(c.question, "Delete space globex?") {
		t.Fatalf("d on the empty globex: modal %#v, want it to ask", m.modal)
	}
	press(m, text("n")...)
	if m.modal != p || len(f.spaceOps) != 0 {
		t.Fatalf("n: modal %T, Space calls %v; want the picker back and nothing deleted", m.modal, f.spaceOps)
	}
	press(m, text("d")...)
	press(m, text("y")...)
	if want := []string{"delete globex"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Fatalf("Space calls = %v, want %v", f.spaceOps, want)
	}
	if got := len(m.st.SpaceTabs()); got != 2 || m.modal != p || p.cursor != 1 {
		t.Errorf("after deleting: %d Spaces, modal %T, cursor %d; want 2, the picker, cursor on the last", got, m.modal, p.cursor)
	}
	if got := m.st.Space(); got != "" {
		t.Errorf("deleting the active Space left %q active, want the default Space", got)
	}
}

func TestThePickerReordersSpaces(t *testing.T) {
	f := spaceCatalog() // default, acme, globex
	f.spaces = append(f.spaces, Space{Name: "initech"})
	m := newStoreModel(t, f)
	press(m, text("P")...)
	p := m.picker()
	p.cursor = 3
	press(m, keyShiftUp)
	press(m, text("K")...)
	press(m, keyShiftDown)
	press(m, text("J")...)
	press(m, text("J")...) // initech is last again: J does nothing
	if want := []string{"move initech 3", "move initech 2", "move initech 3", "move initech 4"}; !reflect.DeepEqual(f.spaceOps, want) {
		t.Fatalf("Space calls = %v, want %v", f.spaceOps, want)
	}
	p.cursor = 0
	press(m, keyShiftUp) // the first Space: nothing above it
	if len(f.spaceOps) != 4 {
		t.Errorf("shift+up on the first Space called %v", f.spaceOps[4:])
	}
	p.cursor = 2
	press(m, text("K")...)
	var names []string
	for _, tab := range m.st.SpaceTabs() {
		names = append(names, tab.Name)
	}
	if want := []string{"default", "globex", "acme", "initech"}; !reflect.DeepEqual(names, want) || p.cursor != 1 {
		t.Errorf("Spaces %v, cursor %d; want %v with the cursor following globex", names, p.cursor, want)
	}
}

func TestSpacePickerGolden(t *testing.T) {
	m := newSpaceModel(t, 80, 8)
	m.st.SetCursor("staging")
	press(m, keyEnter) // a live Session in the default Space
	press(m, keyPrefix, text("2")[0])
	press(m, text("P")...)
	press(m, keyDown)
	golden(t, "space-picker", m)
}
