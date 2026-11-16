package workspace

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var (
	keyShiftUp   = tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}
	keyShiftDown = tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}
	keyK         = tea.KeyPressMsg{Code: 'k', ShiftedCode: 'K', Mod: tea.ModShift, Text: "K"}
	keyJ         = tea.KeyPressMsg{Code: 'j', ShiftedCode: 'J', Mod: tea.ModShift, Text: "J"}
)

// newReorderModel is an 80x12 Workspace over groupingEntries with the stored
// Groups batch, web and spare (empty). Its rows are:
//
//	0 ▾ batch    3     alpha   6 ▾ ungrouped
//	1     job    4     beta    7     loose
//	2 ▾ web      5 ▾ spare
func newReorderModel(t *testing.T) *groupingModel {
	t.Helper()
	f := &fakeCatalog{entries: groupingEntries, groups: []string{"batch", "web", "spare"}}
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return &groupingModel{Model: m, fakeCatalog: f}
}

func TestGroupMoveKeysMoveTheGroupUnderTheCursor(t *testing.T) {
	up := []string{"[web]", "alpha", "beta", "[batch]", "job", "[spare]", "[]", "loose"}
	down := []string{"[batch]", "job", "[spare]", "[web]", "alpha", "beta", "[]", "loose"}
	tests := []struct {
		name     string
		key      tea.KeyPressMsg
		wantMove [2]any
		wantRows []string
	}{
		{"shift+up", keyShiftUp, [2]any{"web", 1}, up},
		{"K", keyK, [2]any{"web", 1}, up},
		{"shift+down", keyShiftDown, [2]any{"web", 3}, down},
		{"J", keyJ, [2]any{"web", 3}, down},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newReorderModel(t)
			g.st.SetCursorGroup("web")
			press(g.Model, tt.key)
			if want := [][2]any{tt.wantMove}; !reflect.DeepEqual(g.groupMoves, want) {
				t.Errorf("MoveGroup calls = %v, want %v", g.groupMoves, want)
			}
			if got := g.rows(); !reflect.DeepEqual(got, tt.wantRows) {
				t.Errorf("rows = %v, want %v", got, tt.wantRows)
			}
			if r, ok := g.st.CursorRow(); !ok || !r.Header() || r.Group != "web" {
				t.Errorf("cursor = %+v, want the web header", r)
			}
		})
	}
}

func TestGroupMoveKeysAtTheEdgesOrOffAGroupHeaderDoNothing(t *testing.T) {
	tests := []struct {
		name  string
		setup func(s *State)
		key   tea.KeyPressMsg
	}{
		{"first group up", func(s *State) { s.SetCursorGroup("batch") }, keyShiftUp},
		{"last group down", func(s *State) { s.SetCursorGroup("spare") }, keyJ},
		{"connection row K", func(s *State) { s.SetCursor("alpha") }, keyK},
		{"connection row shift+down", func(s *State) { s.SetCursor("alpha") }, keyShiftDown},
		{"ungrouped header", func(s *State) { s.SetCursorGroup("") }, keyShiftUp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newReorderModel(t)
			tt.setup(g.st)
			before := g.rows()
			press(g.Model, tt.key)
			if len(g.groupMoves) != 0 {
				t.Errorf("MoveGroup calls = %v, want none", g.groupMoves)
			}
			if got := g.rows(); !reflect.DeepEqual(got, before) {
				t.Errorf("rows = %v, want %v", got, before)
			}
		})
	}
}

func TestGroupMoveSaveErrorShowsAndKeepsTheOrder(t *testing.T) {
	g := newReorderModel(t)
	g.moveGroupErr = errors.New("disk full")
	g.st.SetCursorGroup("web")
	before := g.rows()
	press(g.Model, keyK)
	if !g.statusErr || !strings.Contains(g.status, "disk full") {
		t.Errorf("status = %q (err %v), want the save error", g.status, g.statusErr)
	}
	if got := g.rows(); !reflect.DeepEqual(got, before) {
		t.Errorf("rows = %v, want %v", got, before)
	}
}

func TestDragGroupHeaderOntoAnotherHeaderMovesTheGroup(t *testing.T) {
	tests := []struct {
		name  string
		from  int
		to    int
		want  [2]any
		first string
	}{
		{"up onto the first", 5, 0, [2]any{"spare", 1}, "[spare]"},
		{"down onto the last", 0, 5, [2]any{"batch", 3}, "[web]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newReorderModel(t)
			g.Update(tea.MouseClickMsg{X: 3, Y: tt.from, Button: tea.MouseLeft})
			g.Update(tea.MouseMotionMsg{X: 3, Y: 2, Button: tea.MouseLeft})
			g.Update(tea.MouseMotionMsg{X: 3, Y: tt.to, Button: tea.MouseLeft})
			if !strings.Contains(g.renderStatus(80), "release to drop") {
				t.Errorf("status = %q, want a drop hint", g.renderStatus(80))
			}
			g.Update(tea.MouseReleaseMsg{X: 3, Y: tt.to, Button: tea.MouseLeft})
			if want := [][2]any{tt.want}; !reflect.DeepEqual(g.groupMoves, want) {
				t.Errorf("MoveGroup calls = %v, want %v", g.groupMoves, want)
			}
			if got := g.rows()[0]; got != tt.first {
				t.Errorf("first row = %q, want %q", got, tt.first)
			}
			if len(g.collapsed) != 0 {
				t.Errorf("a drag must not toggle the group; saved collapsed %v", g.collapsed)
			}
		})
	}
}

func TestDragGroupHeaderOffAnotherHeaderChangesNothing(t *testing.T) {
	tests := []struct {
		name string
		x, y int
	}{
		{"onto a member", 3, 3},
		{"onto the ungrouped header", 3, 6},
		{"back onto itself", 3, 2},
		{"off the list", 50, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newReorderModel(t)
			before := g.rows()
			g.Update(tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}) // web
			g.Update(tea.MouseMotionMsg{X: 3, Y: 1, Button: tea.MouseLeft})
			g.Update(tea.MouseMotionMsg{X: tt.x, Y: tt.y, Button: tea.MouseLeft})
			g.Update(tea.MouseReleaseMsg{X: tt.x, Y: tt.y, Button: tea.MouseLeft})
			if len(g.groupMoves) != 0 || len(g.moves) != 0 || len(g.collapsed) != 0 {
				t.Errorf("group moves %v, moves %v, collapsed %v; want none", g.groupMoves, g.moves, g.collapsed)
			}
			if got := g.rows(); !reflect.DeepEqual(got, before) {
				t.Errorf("rows = %v, want %v", got, before)
			}
			if g.drag != nil {
				t.Error("the drag should have ended")
			}
		})
	}
}
