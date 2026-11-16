package workspace

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
)

// newGroupModel is a Workspace over groupEntries with the stored Groups web
// and batch, the cursor on web's header.
func newGroupModel(t *testing.T) (*Model, *fakeCatalog) {
	t.Helper()
	f := &fakeCatalog{entries: append([]Entry(nil), groupEntries...), groups: []string{"web", "batch"}}
	m := newStoreModel(t, f)
	m.st.SetCursorGroup("web")
	return m, f
}

func TestDeleteOnAGroupHeaderAsksThenUngroupsItsMembers(t *testing.T) {
	m, f := newGroupModel(t)
	press(m, text("d")...)
	want := "Delete group web? 4 connections become ungrouped. (y/n)"
	if m.confirm() == nil || m.confirm().question != want {
		t.Fatalf("confirm = %+v, want %q", m.confirm(), want)
	}
	press(m, text("y")...)
	if !reflect.DeepEqual(f.groupOps, []string{"delete web"}) {
		t.Fatalf("group calls = %v", f.groupOps)
	}
	if m.st.HasGroup("web") || m.st.Get("alpha").Group != "" {
		t.Errorf("web still listed: groups %v, alpha in %q", m.st.Groups(), m.st.Get("alpha").Group)
	}
	if m.statusErr || m.status != "deleted group web" {
		t.Errorf("status = %q (err %v)", m.status, m.statusErr)
	}
}

func TestAddOnAGroupHeaderAddsAGroup(t *testing.T) {
	m, f := newGroupModel(t)
	press(m, keyPrefix, text("a")[0])
	if got, _ := m.modal.status(); !strings.HasPrefix(got, "new group: █") {
		t.Fatalf("status line = %q, want the new group prompt", got)
	}
	press(m, text("dev")...)
	press(m, keyEnter)
	if !reflect.DeepEqual(f.groupOps, []string{"add dev"}) || len(f.saves) != 0 {
		t.Fatalf("group calls = %v saves = %v", f.groupOps, f.saves)
	}
	if want := []string{"web", "batch", "dev"}; m.modal != nil || !reflect.DeepEqual(m.st.Groups(), want) {
		t.Fatalf("modal = %v groups = %v, want %v", m.modal, m.st.Groups(), want)
	}
	if r, _ := m.st.CursorRow(); !r.Header() || r.Group != "dev" {
		t.Errorf("cursor on %+v, want dev's header", r)
	}
	if m.statusErr || m.status != "added group dev" {
		t.Errorf("status = %q (err %v)", m.status, m.statusErr)
	}
}

// Away from a Group's header the keys act on Connections as before: on a
// Connection row, on the ungrouped section's header, and on the Active
// connection while the terminal has focus.
func TestCrudKeysOffAGroupHeaderActOnConnections(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *Model)
		key   string
		check func(m *Model) bool
	}{
		{"a on a connection", func(m *Model) { m.st.SetCursor("alpha") }, "a",
			func(m *Model) bool { return m.form() != nil && m.form().old == "" && m.form().spec().Group == "web" }},
		{"a on the ungrouped header", func(m *Model) { m.st.SetCursorGroup("") }, "a",
			func(m *Model) bool { return m.form() != nil && m.form().spec().Group == "" }},
		{"e on the ungrouped header", func(m *Model) { m.st.SetCursorGroup("") }, "e",
			func(m *Model) bool { return m.modal == nil && m.status == "no connection to edit" }},
		{"e with the terminal focused", func(m *Model) {
			m.st.OpenSession("alpha")
			m.focus = focusPane
			m.st.SetCursorGroup("web")
		}, "e", func(m *Model) bool { return m.form() != nil && m.form().old == "alpha" }},
		{"d on a connection", func(m *Model) { m.st.SetCursor("db") }, "d",
			func(m *Model) bool {
				return m.confirm() != nil && strings.HasPrefix(m.confirm().question, "Remove db's")
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, f := newGroupModel(t)
			tt.setup(m)
			press(m, keyPrefix, text(tt.key)[0])
			if !tt.check(m) || len(f.groupOps) != 0 {
				t.Fatalf("modal = %T status = %q group calls = %v", m.modal, m.status, f.groupOps)
			}
		})
	}
}

// The form saves only into an existing Group; for a new name it asks to
// create the Group first, and saves again on yes.
func TestTheFormOffersToCreateANewGroup(t *testing.T) {
	unknown := fmt.Errorf("%q: %w", "dev", catalog.ErrUnknownGroup)
	for _, answer := range []string{"y", "n"} {
		t.Run(answer, func(t *testing.T) {
			m, f := newGroupModel(t)
			f.saveErrs = []error{unknown}
			m.st.SetCursor("db")
			press(m, text("e")...)
			fill(m, "", "dev")
			press(m, keyCtrlS)
			if want := "Create group dev? (y/n)"; m.confirm() == nil || m.confirm().question != want {
				t.Fatalf("confirm = %+v, want %q", m.confirm(), want)
			}
			press(m, text(answer)...)
			if answer == "n" {
				if m.form() == nil || m.form().err != "no group dev" || len(f.groupOps) != 0 {
					t.Fatalf("form = %+v group calls = %v", m.form(), f.groupOps)
				}
				return
			}
			if !reflect.DeepEqual(f.groupOps, []string{"add dev"}) || !reflect.DeepEqual(f.saves, []string{"db→db", "db→db"}) {
				t.Fatalf("group calls = %v saves = %v", f.groupOps, f.saves)
			}
			if m.form() != nil || m.st.Get("db").Group != "dev" || !m.st.HasGroup("dev") {
				t.Fatalf("form = %+v db in %q", m.form(), m.st.Get("db").Group)
			}
		})
	}
}

func TestEditOnAGroupHeaderRenamesIt(t *testing.T) {
	m, f := newGroupModel(t)
	press(m, text("h")...) // collapse web; it stays collapsed under its new name
	press(m, text("e")...)
	if got, _ := m.modal.status(); !strings.HasPrefix(got, "rename group web to: web█") {
		t.Fatalf("status line = %q, want the rename prompt", got)
	}
	press(m, keyClear)
	press(m, text("front")...)
	press(m, keyEnter)
	if !reflect.DeepEqual(f.groupOps, []string{"rename web→front"}) {
		t.Fatalf("group calls = %v", f.groupOps)
	}
	if m.modal != nil || m.st.HasGroup("web") || m.st.Get("alpha").Group != "front" {
		t.Fatalf("modal = %v groups = %v alpha in %q", m.modal, m.st.Groups(), m.st.Get("alpha").Group)
	}
	if r, _ := m.st.CursorRow(); !r.Header() || r.Group != "front" {
		t.Errorf("cursor on %+v, want front's header", r)
	}
	if !m.st.Collapsed("front") || m.st.Collapsed("web") {
		t.Errorf("collapsed = %v, want front", m.st.CollapsedGroups())
	}
	if m.statusErr || m.status != "renamed group web to front" {
		t.Errorf("status = %q (err %v)", m.status, m.statusErr)
	}
}

func TestARefusedGroupNameKeepsThePromptOpen(t *testing.T) {
	m, f := newGroupModel(t)
	f.groupErr = errors.New(`"batch": a group with that name already exists`)
	press(m, text("e")...)
	press(m, keyClear)
	press(m, text("batch")...)
	press(m, keyEnter)
	got, _ := m.modal.status()
	if !strings.Contains(got, "batch█") || !strings.Contains(got, "already exists") {
		t.Fatalf("status line = %q, want the prompt with the error", got)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.modal != nil || !m.st.HasGroup("web") {
		t.Fatalf("modal = %v groups = %v", m.modal, m.st.Groups())
	}
}
