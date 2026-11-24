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
