package workspace

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// listText is the connection list as rendered, without styling, one line
// per row.
func listText(m *Model) []string {
	var out []string
	for _, l := range strings.Split(ansi.Strip(m.render()), "\n") {
		left, _, _ := strings.Cut(l, "│")
		out = append(out, strings.TrimRight(left, " "))
	}
	return out
}

func TestSShowsTheSpaceNamesUntilTheNextKey(t *testing.T) {
	m := newSpaceModel(t, 100, 10) // 1 default, 2 acme, 5 idle
	press(m, text("S")...)
	lines := listText(m)
	// Under the Space bar and its rule.
	if want := " 1 default · 2 acme · 5 idle"; len(lines) < 3 || lines[2] != want {
		t.Fatalf("list after S:\n%s\nwant line 3 %q", strings.Join(lines, "\n"), want)
	}
	press(m, keyDown)
	if lines := listText(m); strings.Contains(strings.Join(lines, "\n"), "2 acme") {
		t.Errorf("the note is still shown after the next key:\n%s", strings.Join(lines, "\n"))
	}

	m.focus = focusPane
	press(m, keyPrefix, text("S")[0])
	if !strings.Contains(strings.Join(listText(m), "\n"), "1 default · 2 acme") {
		t.Errorf("prefix S in the pane did not show the note")
	}
}

func TestTheSpaceNoteDisappearsAfterItsTimeout(t *testing.T) {
	m := newSpaceModel(t, 100, 10)
	press(m, text("S")...)
	stale := hideSpaceNoteMsg{id: m.spaceNoteID}
	press(m, keyDown, text("S")[0]) // a second note replaces the first
	m.Update(stale)
	if !m.spaceNote {
		t.Fatal("the first note's timeout hid the second note")
	}
	m.Update(hideSpaceNoteMsg{id: m.spaceNoteID})
	if m.spaceNote {
		t.Error("the note is still shown after its timeout")
	}
}

func TestTheSpaceNoteWrapsBetweenSpacesInANarrowList(t *testing.T) {
	m := newSpaceModel(t, 80, 10)
	m.opts.ListWidth = 16
	press(m, text("S")...)
	lines := listText(m)
	if want := []string{" 1 default ·", " 2 acme · 5 idle"}; len(lines) < 4 || lines[2] != want[0] || lines[3] != want[1] {
		t.Fatalf("list after S:\n%s\nwant lines 3-4 %q", strings.Join(lines, "\n"), want)
	}
	golden(t, "space-note", m)
}

func TestTheSpaceNoteGoesOnTheStatusLineWhileTheListIsHidden(t *testing.T) {
	m := newSpaceModel(t, 100, 10)
	press(m, keyPrefix, text("b")[0]) // hide the list
	press(m, keyPrefix, text("S")[0])
	if want := "1 default · 2 acme · 5 idle"; !strings.Contains(m.renderStatus(m.w), want) {
		t.Fatalf("status line %q, want it to show %q", ansi.Strip(m.renderStatus(m.w)), want)
	}
	press(m, keyDown)
	if strings.Contains(m.renderStatus(m.w), "2 acme") {
		t.Errorf("the next key left the note on the status line: %q", ansi.Strip(m.renderStatus(m.w)))
	}
}
