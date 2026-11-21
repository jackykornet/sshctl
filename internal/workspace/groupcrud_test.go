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
	want := "Delete group web? 4 connections become ungrouped."
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
	if got, _ := m.mode.modal.status(); !strings.HasPrefix(got, "new group: █") {
		t.Fatalf("status line = %q, want the new group prompt", got)
	}
	press(m, text("dev")...)
	press(m, keyEnter)
	if !reflect.DeepEqual(f.groupOps, []string{"add dev"}) || len(f.saves) != 0 {
		t.Fatalf("group calls = %v saves = %v", f.groupOps, f.saves)
	}
	if want := []string{"web", "batch", "dev"}; m.mode.modal != nil || !reflect.DeepEqual(m.st.Groups(), want) {
		t.Fatalf("modal = %v groups = %v, want %v", m.mode.modal, m.st.Groups(), want)
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
			func(m *Model) bool { return m.mode.modal == nil && m.status == "no connection to edit" }},
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
				t.Fatalf("modal = %T status = %q group calls = %v", m.mode.modal, m.status, f.groupOps)
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
			if want := "Create group dev?"; m.confirm() == nil || m.confirm().question != want {
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

// Each stored Group's header shows its Group number, which follows a
// reorder or a delete.
func TestHeadersShowGroupNumbers(t *testing.T) {
	m, _ := newGroupModel(t)
	headers := func() []string {
		var out []string
		for _, l := range plainList(m) {
			if _, header, ok := strings.Cut(l, "▾ "); ok {
				out = append(out, strings.TrimSpace(header))
			}
		}
		return out
	}
	if got, want := headers(), []string{"1 web 4", "2 batch 1", "ungrouped 1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("headers = %q, want %q", got, want)
	}
	press(m, tea.KeyPressMsg{Code: 'J', Text: "J"}) // web down below batch
	if got, want := headers(), []string{"1 batch 1", "2 web 4", "ungrouped 1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after reorder headers = %q, want %q", got, want)
	}
	m.st.SetCursorGroup("batch")
	press(m, text("d")...)
	press(m, text("y")...)
	if got, want := headers(), []string{"1 web 4", "ungrouped 2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after delete headers = %q, want %q", got, want)
	}
}

// The move prompt takes a Group number (web is 1, batch 2) as well as a
// name; 0 ungroups.
func TestTheMovePromptTakesAGroupNumber(t *testing.T) {
	tests := []struct {
		typed, want string
	}{
		{"2", "batch"},
		{"0", ""},
		{"batch", "batch"},
	}
	for _, tt := range tests {
		t.Run(tt.typed, func(t *testing.T) {
			m, f := newGroupModel(t)
			m.st.SetCursor("alpha")
			press(m, keyPrefix, text("m")[0], keyClear)
			press(m, append(text(tt.typed), keyEnter)...)
			if got := m.st.Get("alpha").Group; got != tt.want {
				t.Fatalf("alpha in %q, want %q (status %q)", got, tt.want, m.status)
			}
			if want := [][2]string{{"alpha", tt.want}}; !reflect.DeepEqual(f.moves, want) {
				t.Errorf("saved %v, want %v", f.moves, want)
			}
		})
	}
}

func TestTheMovePromptRefusesANumberWithNoGroup(t *testing.T) {
	m, f := newGroupModel(t)
	m.st.SetCursor("alpha")
	press(m, keyPrefix, text("m")[0], keyClear)
	press(m, append(text("3"), keyEnter)...)
	if m.st.Get("alpha").Group != "web" || len(f.moves) != 0 || len(f.groupOps) != 0 {
		t.Fatalf("alpha in %q, saved %v, group calls %v", m.st.Get("alpha").Group, f.moves, f.groupOps)
	}
	if !m.statusErr || m.status != "no group number 3" {
		t.Fatalf("status = %q (err %v)", m.status, m.statusErr)
	}
}

// An unknown name in the move prompt asks to create the Group, then moves
// the Connection into it on yes.
func TestTheMovePromptOffersToCreateANewGroup(t *testing.T) {
	for _, answer := range []string{"y", "n"} {
		t.Run(answer, func(t *testing.T) {
			m, f := newGroupModel(t)
			m.st.SetCursor("alpha")
			press(m, keyPrefix, text("m")[0], keyClear)
			press(m, append(text("dev"), keyEnter)...)
			if want := "Create group dev?"; m.confirm() == nil || m.confirm().question != want {
				t.Fatalf("confirm = %+v, want %q", m.confirm(), want)
			}
			press(m, text(answer)...)
			if answer == "n" {
				if m.st.Get("alpha").Group != "web" || len(f.moves) != 0 || len(f.groupOps) != 0 {
					t.Fatalf("alpha in %q, saved %v, group calls %v", m.st.Get("alpha").Group, f.moves, f.groupOps)
				}
				return
			}
			if !reflect.DeepEqual(f.groupOps, []string{"add dev"}) || !reflect.DeepEqual(f.moves, [][2]string{{"alpha", "dev"}}) {
				t.Fatalf("group calls = %v saved %v", f.groupOps, f.moves)
			}
			if m.st.Get("alpha").Group != "dev" || m.st.GroupPosition("dev") != 3 {
				t.Fatalf("alpha in %q, groups %v", m.st.Get("alpha").Group, m.st.Groups())
			}
			if m.statusErr || m.status != "moved alpha to group dev" {
				t.Errorf("status = %q (err %v)", m.status, m.statusErr)
			}
		})
	}
}

// The form's group field takes a Group number too, and lists the numbers
// since the form covers the headers.
func TestTheFormTakesAGroupNumber(t *testing.T) {
	tests := []struct {
		typed, want string
	}{
		{"2", "batch"},
		{"0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.typed, func(t *testing.T) {
			m, f := newGroupModel(t)
			m.st.SetCursor("alpha")
			press(m, text("e")...)
			if !strings.Contains(screen(m), "0 none · 1 web · 2 batch") {
				t.Fatalf("screen = \n%s\nwant the Group numbers listed", screen(m))
			}
			m.form().focus = len(m.form().fields) - 1
			press(m, keyClear)
			press(m, text(tt.typed)...)
			press(m, keyCtrlS)
			if m.form() != nil || len(f.specs) != 1 || f.specs[0].Group != tt.want {
				t.Fatalf("form = %+v specs = %+v, want saved into %q", m.form(), f.specs, tt.want)
			}
		})
	}
}

func TestTheFormRefusesANumberWithNoGroup(t *testing.T) {
	m, f := newGroupModel(t)
	m.st.SetCursor("alpha")
	press(m, text("e")...)
	m.form().focus = len(m.form().fields) - 1
	press(m, keyClear)
	press(m, text("7")...)
	press(m, keyCtrlS)
	if m.form() == nil || m.form().err != "no group number 7" || len(f.saves) != 0 {
		t.Fatalf("form = %+v saves = %v", m.form(), f.saves)
	}
}

func TestEditOnAGroupHeaderRenamesIt(t *testing.T) {
	m, f := newGroupModel(t)
	press(m, text("h")...) // collapse web; it stays collapsed under its new name
	press(m, text("e")...)
	if got, _ := m.mode.modal.status(); !strings.HasPrefix(got, "rename group web to: web█") {
		t.Fatalf("status line = %q, want the rename prompt", got)
	}
	press(m, keyClear)
	press(m, text("front")...)
	press(m, keyEnter)
	if !reflect.DeepEqual(f.groupOps, []string{"rename web→front"}) {
		t.Fatalf("group calls = %v", f.groupOps)
	}
	if m.mode.modal != nil || m.st.HasGroup("web") || m.st.Get("alpha").Group != "front" {
		t.Fatalf("modal = %v groups = %v alpha in %q", m.mode.modal, m.st.Groups(), m.st.Get("alpha").Group)
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
	got, _ := m.mode.modal.status()
	if !strings.Contains(got, "batch█") || !strings.Contains(got, "already exists") {
		t.Fatalf("status line = %q, want the prompt with the error", got)
	}
	press(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode.modal != nil || !m.st.HasGroup("web") {
		t.Fatalf("modal = %v groups = %v", m.mode.modal, m.st.Groups())
	}
}
