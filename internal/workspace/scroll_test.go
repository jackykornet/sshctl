package workspace

import (
	"fmt"
	"strings"
	"testing"
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
	for _, l := range strings.Split(screen(m), "\n")[1 : m.h-1] {
		_, pane, _ := strings.Cut(l, "│")
		rows = append(rows, strings.TrimRight(pane, " "))
	}
	return rows
}

func TestScrolledBackViewStaysAsOutputArrives(t *testing.T) {
	tests := []struct {
		name string
		keys string // in scroll mode
		// grow is how far back the view must be after five more lines.
		grow int
	}{
		{name: "a few lines back", keys: "kkk", grow: 5},
		{name: "at the top of history", keys: "g", grow: 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, sess := newPaneModel(t, "")
			printLines(m, sess, 1, 40)
			press(m, keyPrefix, text("s")[0])
			press(m, text(tc.keys)...)
			r := m.sessions.runner(sess)
			before, back := paneBody(m), r.scroll

			printLines(m, sess, 41, 45)

			if got := paneBody(m); strings.Join(got, "\n") != strings.Join(before, "\n") {
				t.Fatalf("view moved:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(before, "\n"))
			}
			if r.scroll != back+tc.grow {
				t.Fatalf("scroll = %d, want %d", r.scroll, back+tc.grow)
			}
			want := fmt.Sprintf("scroll: %d/%d lines back", back+tc.grow, r.term.ScrollbackLen())
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
