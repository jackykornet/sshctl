package workspace

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newEmptyModel is a Workspace with no Connections at all, w by h.
func newEmptyModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m, err := New(Options{Catalog: &fakeCatalog{identity: "/home/me/.ssh/id_ed25519"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func TestGettingStartedGolden(t *testing.T) {
	golden(t, "getting-started", newEmptyModel(t, 80, 24))
}

func TestGettingStartedInAnEmptySpaceGolden(t *testing.T) {
	m := newSpaceModel(t, 80, 24)
	press(m, text("5")...)
	golden(t, "getting-started-space", m)
}

// A filter that matches nothing keeps the pane's usual message, even with
// no Connections.
func TestGettingStartedHidesWhileFiltering(t *testing.T) {
	m := newEmptyModel(t, 80, 24)
	press(m, text("/x")...)
	s := screen(m)
	if strings.Contains(s, "No connections") {
		t.Errorf("getting-started screen shown while filtering:\n%s", s)
	}
	if !strings.Contains(s, "no matches") {
		t.Errorf("list lacks its no matches message:\n%s", s)
	}
}

// In a Space other than the default, while ssh_config's hosts live in
// another Space, the empty list does not say ssh_config has none.
func TestAnEmptySpaceDoesNotSaySSHConfigIsEmpty(t *testing.T) {
	m := newSpaceModel(t, 80, 24)
	press(m, text("5")...)
	if s := screen(m); strings.Contains(s, "no hosts in ssh_config") {
		t.Errorf("an empty Space says ssh_config has no hosts:\n%s", s)
	}
	if s := screen(newEmptyModel(t, 80, 24)); !strings.Contains(s, "no hosts in ssh_config") {
		t.Errorf("a Workspace with no hosts lacks its message:\n%s", s)
	}
}

// In a Space whose only Groups are empty, the cursor is on a Group header,
// where a adds a Group: the getting-started screen says so, not that it
// adds a Connection.
func TestGettingStartedOnAGroupHeaderNamesWhatAAdds(t *testing.T) {
	m, err := New(Options{Catalog: &fakeCatalog{
		entries:     []Entry{{Alias: "web"}},
		spaces:      []Space{{Name: "idle", Number: 5}},
		spaceGroups: []Group{{Name: "prod", Space: "idle"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	press(m, text("5")...)
	if _, ok := m.headerTarget(); !ok {
		t.Fatal("the cursor is not on prod's header")
	}
	golden(t, "getting-started-group", m)
	s := strings.Join(strings.Fields(screen(m)), " ")
	if strings.Contains(s, "a add: a new connection") {
		t.Errorf("on a Group header the screen says a adds a connection:\n%s", screen(m))
	}
	if !strings.Contains(s, "a add: a new group") {
		t.Errorf("on a Group header the screen does not say a adds a group:\n%s", screen(m))
	}
	press(m, text("a")...)
	if _, ok := m.mode.modal.(*groupInput); !ok {
		t.Errorf("a opened %T, want the new group prompt", m.mode.modal)
	}
}
