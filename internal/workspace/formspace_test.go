package workspace

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
)

// formValues returns the open form's values by label.
func formValues(m *Model) map[string]string {
	out := map[string]string{}
	for _, fl := range m.form().fields {
		out[fl.label] = fl.value.String()
	}
	return out
}

// focusFormSpace puts the form's focus on its Space field.
func focusFormSpace(m *Model) {
	f := m.form()
	for i, fl := range f.fields {
		if fl.id == fieldSpace {
			f.focusField(i)
		}
	}
}

func TestTheFormsSpaceFieldStartsOnTheActiveSpace(t *testing.T) {
	f := spaceCatalog() // default (web, mine; Group prod), acme (db; Group data), globex
	f.activeSpace = "acme"
	m := newStoreModel(t, f)
	m.st.SetCursor("db")
	press(m, text("a")...)
	if got := formValues(m); got["Space"] != "acme" || got["Group"] != "data" {
		t.Fatalf("add form Space %q, Group %q; want acme and data", got["Space"], got["Group"])
	}
	press(m, keyEsc)
	press(m, text("e")...)
	if got := formValues(m); got["Space"] != "acme" {
		t.Errorf("edit form for db: Space %q, want acme", got["Space"])
	}
}

func TestChangingTheFormsSpaceClearsItsGroupAndSavesThere(t *testing.T) {
	f := spaceCatalog()
	f.activeSpace = "acme"
	m := newStoreModel(t, f)
	m.st.SetCursor("db")
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	focusFormSpace(m)
	press(m, keyClear)
	press(m, text("1")...) // the default Space, by its number
	if got := formValues(m)["Group"]; got != "" {
		t.Fatalf("after changing the Space, Group = %q, want it cleared", got)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	press(m, text("1")...) // Group 1 of the default Space, prod
	press(m, keyCtrlS)
	if len(f.specs) != 1 || f.specs[0].Space != "" || f.specs[0].Group != "prod" {
		t.Fatalf("saved %+v, want api in the default Space's prod", f.specs)
	}
	if m.form() != nil {
		t.Fatalf("form still open: %s", m.form().err)
	}
	if !strings.Contains(m.status, "default") {
		t.Errorf("status %q, want it to say api went to the default Space", m.status)
	}
}

func TestEditingTheSpaceMovesOnlyTheMetadata(t *testing.T) {
	f := spaceCatalog()
	f.entries[len(f.entries)-1].Managed = true
	f.entries[len(f.entries)-1].Host = Host{HostName: "10.1.1.1"}
	f.activeSpace = "acme"
	m := newStoreModel(t, f)
	m.st.SetCursor("db")
	press(m, text("e")...)
	focusFormSpace(m)
	press(m, keyClear)
	press(m, text("globex")...)
	press(m, keyCtrlS)
	want := Spec{Alias: "db", Space: "globex", Managed: true, Host: Host{HostName: "10.1.1.1"}}
	if len(f.specs) != 1 || !reflect.DeepEqual(f.specs[0], want) {
		t.Fatalf("saved %+v, want %+v", f.specs, want)
	}
	if got := aliases(m.st.Rows()); m.st.Space() != "globex" || !reflect.DeepEqual(got, []string{"db"}) {
		t.Errorf("after moving db: Space %q lists %v, want globex listing db", m.st.Space(), got)
	}
}

func TestTheFormRefusesAnUnknownSpace(t *testing.T) {
	f := spaceCatalog()
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	focusFormSpace(m)
	press(m, keyClear)
	press(m, text("nope")...)
	press(m, keyCtrlS)
	if len(f.specs) != 0 || m.form() == nil || m.form().err != "no space nope" {
		t.Errorf("saves %v, form %+v; want the form open saying no space nope", f.specs, m.form())
	}
}

func TestAnUnknownGroupIsCreatedInTheFormsSpace(t *testing.T) {
	f := spaceCatalog()
	f.saveErrs = []error{fmt.Errorf("%q: %w", "ops", catalog.ErrUnknownGroup)}
	m := newStoreModel(t, f)
	m.st.SetCursor("web")
	press(m, text("a")...)
	fill(m, "api", "10.0.0.9")
	focusFormSpace(m)
	press(m, keyClear)
	press(m, text("globex")...)
	press(m, tea.KeyPressMsg{Code: tea.KeyTab})
	press(m, text("ops")...)
	press(m, keyCtrlS)
	press(m, text("y")...)
	if want := []string{"add ops"}; !reflect.DeepEqual(f.groupOps, want) || !reflect.DeepEqual(f.groupSpaces, []string{"globex"}) {
		t.Errorf("group calls %v in Spaces %q, want add ops in globex", f.groupOps, f.groupSpaces)
	}
}

func TestTheFormHasNoSpaceFieldWithOnlyTheDefaultSpace(t *testing.T) {
	m := newStoreModel(t, &fakeCatalog{entries: storeEntries()})
	m.st.SetCursor("web")
	press(m, text("a")...)
	if _, ok := formValues(m)["Space"]; ok {
		t.Error("the add form has a Space field while there is one Space")
	}
}

func TestFormSpaceGolden(t *testing.T) {
	m := newSpaceModel(t, 80, 18)
	m.st.SetCursor("staging")
	press(m, text("a")...)
	focusFormSpace(m)
	golden(t, "form-space", m)
}

func TestSavingIntoAnotherSpaceShowsThatSpace(t *testing.T) {
	m, r := newRecordingModel(t, []Entry{{Alias: "web"}, {Alias: "db", Space: "acme"}})
	r.spaces = []Space{{Name: "acme"}}
	press(m, keyPrefix, text("R")[0]) // reload to see acme
	m.st.SetCursor("web")
	press(m, keyEnter) // web has a live tab
	press(m, keyPrefix, text("e")[0])
	focusFormSpace(m)
	press(m, keyClear)
	press(m, text("acme")...)
	press(m, keyCtrlS)
	if got := m.st.Space(); got != "acme" {
		t.Fatalf("after moving web, active Space = %q, want acme", got)
	}
	if c := m.st.Active(); c == nil || c.Alias != "web" || m.focus != focusPane {
		t.Errorf("Active connection %v, focus %v; want web with its tab, in the pane", c, m.focus)
	}

	// Ctrl+O into the default Space: the Bootstrap tab opens there, in view.
	m.st.SetCursor("db")
	m.focus = focusList
	press(m, text("a")...)
	fill(m, "new", "10.0.0.2")
	focusFormSpace(m)
	press(m, keyClear)
	press(m, text("1")...)
	press(m, tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if got := m.st.Space(); got != "" {
		t.Fatalf("after Ctrl+O into the default Space, active Space = %q", got)
	}
	if c := m.st.Active(); c == nil || c.Alias != "new" || c.CurrentSession().Action != ActionBootstrap {
		t.Errorf("Active connection %v, want new with its Bootstrap tab", c)
	}
	m.st.SetSpace("acme")
	if c := m.st.Active(); c == nil || c.Alias != "web" {
		t.Errorf("acme's Active connection = %v, want web kept", c)
	}
}
