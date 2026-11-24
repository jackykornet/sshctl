package workspace

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// printLines writes "line from" to "line to" to sess's emulator, as program
// output, and delivers the redraw it causes.
func printLines(m *Model, sess *Session, from, to int) {
	r := m.sessions.runner(sess)
	for i := from; i <= to; i++ {
		fmt.Fprintf(r.term, "line %d\r\n", i)
	}
	r.activity.Store(true)
	m.Update(redrawMsg{})
}

// paneBody returns the Session pane's rows below the tab bar, as shown.
func paneBody(m *Model) []string {
	var rows []string
	for _, l := range strings.Split(screen(m), "\n")[m.frame().bodyTop:m.frame().mainEnd()] {
		_, pane, _ := strings.Cut(l, "│")
		rows = append(rows, strings.TrimRight(pane, " "))
	}
	return rows
}

func TestScrolledBackViewStaysAsOutputArrives(t *testing.T) {
	tests := []struct {
		name string
		keys string // in scroll mode
	}{
		{name: "a few lines back", keys: "kkk"},
		{name: "at the top of history", keys: "g"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, sess := newPaneModel(t, "")
			printLines(m, sess, 1, 40)
			press(m, keyPrefix, text("s")[0])
			press(m, text(tc.keys)...)
			r := m.sessions.runner(sess)
			before, back := paneBody(m), r.scroll

			printLines(m, sess, 41, 45) // five lines more history

			if got := paneBody(m); strings.Join(got, "\n") != strings.Join(before, "\n") {
				t.Fatalf("view moved:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(before, "\n"))
			}
			if r.scroll != back+5 {
				t.Fatalf("scroll = %d, want %d", r.scroll, back+5)
			}
			want := fmt.Sprintf("scroll: %d/%d lines back", back+5, r.term.ScrollbackLen())
			if !strings.Contains(screen(m), want) {
				t.Fatalf("status should say %q:\n%s", want, screen(m))
			}
		})
	}
}

func TestLiveViewFollowsOutput(t *testing.T) {
	m, sess := newPaneModel(t, "")
	printLines(m, sess, 1, 40)
	printLines(m, sess, 41, 45)
	if r := m.sessions.runner(sess); r.scroll != 0 || !strings.Contains(screen(m), "line 45") {
		t.Fatalf("a view that is not scrolled back shows the newest output (scroll %d):\n%s", r.scroll, screen(m))
	}
}

func TestSelectionOnAScrolledBackViewCopiesWhatWasSelected(t *testing.T) {
	m, sess := newPaneModel(t, "")
	printLines(m, sess, 1, 40)
	press(m, keyPrefix, text("s")[0])
	press(m, text("g")...) // line 1 to line 10 are shown
	m.Update(click(29, 2, tea.MouseLeft))
	m.Update(motion(34, 3))
	// Output arrives before the release, and before its redraw.
	r := m.sessions.runner(sess)
	fmt.Fprint(r.term, "line 41\r\nline 42\r\n")
	_, cmd := m.Update(release(34, 3))
	if got, ok := clipboardSet(cmd); !ok || got != "line 1\nline 2" {
		t.Fatalf("clipboard = %q, %v; want the lines that were selected", got, ok)
	}
}

func TestTopOfHistoryStaysWhenOutputArrivesBeforeTheNextDraw(t *testing.T) {
	m, sess := newPaneModel(t, "")
	printLines(m, sess, 1, 40)
	press(m, keyPrefix, text("s")[0])
	press(m, text("g")...)
	fmt.Fprint(m.sessions.runner(sess).term, "line 41\r\n") // no Update follows
	if got := paneBody(m)[0]; got != "line 1" {
		t.Fatalf("top row = %q, want line 1:\n%s", got, screen(m))
	}
}

func TestScrollStepAfterNewOutput(t *testing.T) {
	m, sess := newPaneModel(t, "")
	printLines(m, sess, 1, 40)
	press(m, keyPrefix, text("s")[0])
	r := m.sessions.runner(sess)
	fmt.Fprint(r.term, "line 41\r\nline 42\r\n") // arrives before k
	press(m, text("k")...)
	if r.scroll != 1 {
		t.Fatalf("scroll = %d after one step back, want 1", r.scroll)
	}
	if got := paneBody(m)[len(paneBody(m))-1]; got != "line 42" {
		t.Fatalf("bottom row = %q, want line 42:\n%s", got, screen(m))
	}
}
