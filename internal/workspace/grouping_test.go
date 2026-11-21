package workspace

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

var keyClear = tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}

// groupingEntries list as these screen rows when nothing is collapsed:
//
//	0 ▾ batch    3     alpha   6     loose
//	1     job    4     beta
//	2 ▾ web      5 ▾ ungrouped
var groupingEntries = []Entry{
	{Alias: "job", Group: "batch"},
	{Alias: "alpha", Group: "web"},
	{Alias: "beta", Group: "web"},
	{Alias: "loose"},
}

// groupingModel is an 80x12 Workspace over groupingEntries, starting with
// the collapsed groups, whose fakeCatalog records every SetGroup (moves) and
// SetCollapsed (collapsed) call; SetGroup fails when failGroup is set.
type groupingModel struct {
	*Model
	*fakeCatalog
}

func newGroupingModel(t *testing.T, collapsed ...string) *groupingModel {
	t.Helper()
	f := &fakeCatalog{entries: groupingEntries, startCollapsed: collapsed}
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return &groupingModel{Model: m, fakeCatalog: f}
}

func (g *groupingModel) rows() []string { return rowNames(g.st.Rows()) }

func TestEnterOnHeaderCollapsesAndSavesGroup(t *testing.T) {
	g := newGroupingModel(t)
	press(g.Model, keyEnter) // cursor starts on the batch header
	if got, want := g.rows(), []string{"[batch]", "[web]", "alpha", "beta", "[]", "loose"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	press(g.Model, keyEnter)
	if want := [][]string{{"batch"}, {}}; !reflect.DeepEqual(g.collapsed, want) {
		t.Fatalf("saved %v, want %v", g.collapsed, want)
	}
	if g.st.Active() != nil {
		t.Fatal("toggling a header must not open anything")
	}
}

func TestLeftAndRightCollapseAndExpand(t *testing.T) {
	g := newGroupingModel(t)
	g.st.SetCursor("beta")
	press(g.Model, tea.KeyPressMsg{Code: tea.KeyLeft}) // from a member collapses its group
	if r, _ := g.st.CursorRow(); !g.st.Collapsed("web") || !r.Header() || r.Group != "web" {
		t.Fatalf("collapsed = %v cursor = %+v", g.st.Collapsed("web"), r)
	}
	press(g.Model, tea.KeyPressMsg{Code: tea.KeyRight})
	if g.st.Collapsed("web") {
		t.Fatal("right should expand the group")
	}
	if len(g.collapsed) != 2 {
		t.Fatalf("saved %d times, want 2", len(g.collapsed))
	}
}

func TestCollapsedOptionStartsGroupsCollapsed(t *testing.T) {
	g := newGroupingModel(t, "web")
	if got, want := g.rows(), []string{"[batch]", "job", "[web]", "[]", "loose"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if len(g.collapsed) != 0 {
		t.Fatal("starting collapsed must not save")
	}
}

func TestClickHeaderTogglesGroup(t *testing.T) {
	g := newGroupingModel(t)
	g.Update(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft}) // web header
	g.Update(tea.MouseReleaseMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if !g.st.Collapsed("web") {
		t.Fatal("clicking the header should collapse it")
	}
	g.Update(tea.MouseClickMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	g.Update(tea.MouseReleaseMsg{X: 2, Y: 2, Button: tea.MouseLeft})
	if g.st.Collapsed("web") {
		t.Fatal("clicking again should expand it")
	}
}

func TestCollapsedHeaderShowsOpenCount(t *testing.T) {
	g := newGroupingModel(t, "web")
	g.st.OpenSession("alpha")
	if got := plainList(g.Model)[2]; !strings.Contains(got, "▸ web 2 ●1") {
		t.Fatalf("header = %q", got)
	}
}

func TestPrefixMMovesConnectionToTypedGroup(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyPressMsg
		group string
	}{
		{"existing group", append(append([]tea.KeyPressMsg{keyClear}, text("batch")...), keyEnter), "batch"},
		{"edit prefilled text", append(append([]tea.KeyPressMsg{{Code: tea.KeyBackspace}, {Code: tea.KeyBackspace}, {Code: tea.KeyBackspace}}, text("batch")...), keyEnter), "batch"},
		{"empty removes from group", []tea.KeyPressMsg{keyClear, keyEnter}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGroupingModel(t)
			g.st.SetCursor("alpha")
			press(g.Model, keyPrefix, text("m")[0])
			if movePrompt(g.Model) == nil || movePrompt(g.Model).group.String() != "web" {
				t.Fatalf("prompt = %+v, want prefilled with web", movePrompt(g.Model))
			}
			press(g.Model, tt.keys...)
			if movePrompt(g.Model) != nil {
				t.Fatal("Enter should close the prompt")
			}
			if got := g.st.Get("alpha").Group; got != tt.group {
				t.Errorf("group = %q, want %q", got, tt.group)
			}
			if want := [][2]string{{"alpha", tt.group}}; !reflect.DeepEqual(g.moves, want) {
				t.Errorf("saved %v, want %v", g.moves, want)
			}
		})
	}
}

func TestPrefixMEscapeAndSaveErrorLeaveGroupAlone(t *testing.T) {
	g := newGroupingModel(t)
	g.st.SetCursor("alpha")
	press(g.Model, keyPrefix, text("m")[0])
	press(g.Model, text("x")...)
	press(g.Model, tea.KeyPressMsg{Code: tea.KeyEscape})
	if movePrompt(g.Model) != nil || g.st.Get("alpha").Group != "web" || len(g.moves) != 0 {
		t.Fatal("Esc should cancel the move")
	}
	g.failGroup = true
	press(g.Model, keyPrefix, text("m")[0], keyClear)
	press(g.Model, append(text("batch"), keyEnter)...)
	if g.st.Get("alpha").Group != "web" || !g.statusErr || !strings.Contains(g.status, "disk full") {
		t.Fatalf("group = %q status = %q", g.st.Get("alpha").Group, g.status)
	}
}

func TestStoredGroupsListInOrderWithEmptyOnesMarked(t *testing.T) {
	f := &fakeCatalog{entries: groupingEntries, groups: []string{"web", "spare", "batch"}}
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	if got, want := rowNames(m.st.Rows()), []string{"[web]", "alpha", "beta", "[spare]", "[batch]", "job", "[]", "loose"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if got := plainList(m)[3]; !strings.Contains(got, "▾ 2 spare (empty)") {
		t.Fatalf("empty header = %q, want it marked (empty)", got)
	}
}

func TestGroupStaysListedAfterItsLastMemberMovesOut(t *testing.T) {
	f := &fakeCatalog{entries: groupingEntries, groups: []string{"batch", "web"}}
	m, err := New(Options{Catalog: f})
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m.st.SetCursor("job")
	press(m, keyPrefix, text("m")[0], keyClear)
	press(m, append(text("web"), keyEnter)...)
	f.entries = []Entry{{Alias: "job", Group: "web"}, groupingEntries[1], groupingEntries[2], groupingEntries[3]}
	m.reload("", "")
	if got, want := rowNames(m.st.Rows()), []string{"[batch]", "[web]", "alpha", "beta", "job", "[]", "loose"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestStartupShowsTheConfigNotice(t *testing.T) {
	m, err := New(Options{Catalog: &fakeCatalog{}, Notice: `renamed group "2" to "group-2"`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.status, "group-2") {
		t.Fatalf("status = %q, want the notice", m.status)
	}
}

func TestDragConnectionOntoAnotherGroup(t *testing.T) {
	tests := []struct {
		name    string
		dropY   int
		want    string
		wantRow []string
	}{
		{"onto a header", 0, "batch", []string{"[batch]", "alpha", "job", "[web]", "beta", "[]", "loose"}},
		{"onto a member", 6, "", []string{"[batch]", "job", "[web]", "beta", "[]", "alpha", "loose"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGroupingModel(t)
			g.Update(tea.MouseClickMsg{X: 5, Y: 3, Button: tea.MouseLeft}) // alpha
			g.Update(tea.MouseMotionMsg{X: 5, Y: 2, Button: tea.MouseLeft})
			g.Update(tea.MouseMotionMsg{X: 5, Y: tt.dropY, Button: tea.MouseLeft})
			if g.mode.drag == nil || !g.mode.drag.ok || g.mode.drag.target != tt.want {
				t.Fatalf("drag = %+v, want target %q", g.mode.drag, tt.want)
			}
			g.Update(tea.MouseReleaseMsg{X: 5, Y: tt.dropY, Button: tea.MouseLeft})
			if got := g.rows(); !reflect.DeepEqual(got, tt.wantRow) {
				t.Errorf("rows = %v, want %v", got, tt.wantRow)
			}
			if want := [][2]string{{"alpha", tt.want}}; !reflect.DeepEqual(g.moves, want) {
				t.Errorf("saved %v, want %v", g.moves, want)
			}
		})
	}
}

func TestDragOutOfOnlyUngroupedShowsUngroupedTarget(t *testing.T) {
	g := newGroupingModel(t)
	g.st.SetGroup("loose", "batch") // nothing is ungrouped now
	// rows: 0 [batch] 1 job 2 loose 3 [web] 4 alpha 5 beta
	g.Update(tea.MouseClickMsg{X: 5, Y: 4, Button: tea.MouseLeft})
	g.Update(tea.MouseMotionMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	if got := g.rows(); got[len(got)-1] != "[]" {
		t.Fatalf("dragging should list the ungrouped header: %v", got)
	}
	g.Update(tea.MouseReleaseMsg{X: 5, Y: 6, Button: tea.MouseLeft})
	if g.st.Get("alpha").Group != "" {
		t.Fatalf("alpha group = %q, want none", g.st.Get("alpha").Group)
	}
}

func TestDragWithoutMovingOrOffListChangesNothing(t *testing.T) {
	g := newGroupingModel(t)
	g.Update(tea.MouseClickMsg{X: 5, Y: 3, Button: tea.MouseLeft}) // alpha
	g.Update(tea.MouseReleaseMsg{X: 5, Y: 3, Button: tea.MouseLeft})
	g.Update(tea.MouseClickMsg{X: 5, Y: 3, Button: tea.MouseLeft})
	g.Update(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	g.Update(tea.MouseReleaseMsg{X: 50, Y: 0, Button: tea.MouseLeft}) // over the pane
	if len(g.moves) != 0 || g.st.Get("alpha").Group != "web" || g.mode.drag != nil {
		t.Fatalf("moves = %v group = %q", g.moves, g.st.Get("alpha").Group)
	}
	if got, want := g.rows(), []string{"[batch]", "job", "[web]", "alpha", "beta", "[]", "loose"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

func TestPrefixGOnHeaderOpensThatGroup(t *testing.T) {
	m, r := newRecordingModel(t, groupingEntries)
	m.st.SetCollapsed("web", true)
	m.st.SetCursorGroup("web")
	press(m, keyPrefix, keyG)
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(r.started(), want) {
		t.Fatalf("started %v, want %v", r.started(), want)
	}
}

// plainList returns the connection list's lines without styling.
func plainList(m *Model) []string {
	lines := m.renderList(m.listWidth(), m.h-1)
	for i, l := range lines {
		lines[i] = ansi.Strip(l)
	}
	return lines
}

// movePrompt returns the open move prompt, or nil.
func movePrompt(m *Model) *moveInput {
	in, _ := m.mode.modal.(*moveInput)
	return in
}
