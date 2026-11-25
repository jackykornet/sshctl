package workspace

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newPaneModel returns the 80x12 layout Workspace with alpha's Session open
// and focused, showing out. Its runner is an emulator with no process.
func newPaneModel(t *testing.T, out string) (*Model, *Session) {
	t.Helper()
	m, sess, _ := paneModel(t, Options{}, out)
	return m, sess
}

// inputLog records what an emulator sends to its program.
type inputLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write records p.
func (l *inputLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// waitFor waits until the program has been sent want.
func (l *inputLog) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		l.mu.Lock()
		got := l.buf.String()
		l.mu.Unlock()
		if strings.Contains(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("program was sent %q, want it to contain %q", got, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// paneModel is newPaneModel with opts (its Catalog is set here), and it also
// returns what the Session's emulator sends to its program.
func paneModel(t *testing.T, opts Options, out string) (*Model, *Session, *inputLog) {
	t.Helper()
	opts.Catalog = &fakeCatalog{entries: []Entry{{Alias: "alpha"}, {Alias: "beta"}}}
	m, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	press(m, keyEnter)
	sess := m.st.Active().CurrentSession()
	w, h := m.paneSize()
	r := &runner{term: newVterm(w, h, 100, nil)}
	r.term.Write([]byte(strings.ReplaceAll(out, "\n", "\r\n")))
	in := &inputLog{}
	go io.Copy(in, r.term)
	t.Cleanup(func() { r.term.Close() })
	m.sessions.runners[sess] = r
	return m, sess, in
}

// click presses b at screen cell x, y. In these Workspaces the pane body
// starts at column 29, row 2, under the pane header and the tab line.
func click(x, y int, b tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: b}
}

// motion moves the pointer to x, y with the left button held.
func motion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// release lets go of the left button at x, y.
func release(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// dragMouse presses the left button at from, moves to to and releases there.
func dragMouse(m *Model, from, to [2]int) tea.Cmd {
	m.Update(click(from[0], from[1], tea.MouseLeft))
	m.Update(motion(to[0], to[1]))
	_, cmd := m.Update(release(to[0], to[1]))
	return cmd
}

// cmdMsgs runs cmd and the commands of any batch it returns, and collects
// the messages that arrive within a short wait (a status timer does not).
func cmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, cmdMsgs(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

// clipboardSet returns the text cmd puts on the system clipboard with
// tea.SetClipboard, whose message type is not exported.
func clipboardSet(cmd tea.Cmd) (string, bool) {
	for _, msg := range cmdMsgs(cmd) {
		if fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
			return fmt.Sprint(msg), true
		}
	}
	return "", false
}

func TestDragInPaneCopiesSelection(t *testing.T) {
	m, _ := newPaneModel(t, "first line\nsecond line\nthird")
	cmd := dragMouse(m, [2]int{35, 2}, [2]int{33, 4})
	got, ok := clipboardSet(cmd)
	if !ok || got != "line\nsecond line\nthird" {
		t.Fatalf("clipboard = %q, %v", got, ok)
	}
	if m.status != "copied 22 characters" {
		t.Fatalf("status = %q", m.status)
	}
	if m.st.Get("alpha") != m.st.Active() {
		t.Fatal("a drag in the pane must not change the Active connection")
	}
}

// goldenSelection is golden for a screen with a highlighted selection: each
// row with highlighted cells is followed by a row with ^ under each of them.
func goldenSelection(t *testing.T, name string, m *Model) {
	t.Helper()
	var out []string
	for _, line := range strings.Split(m.render(), "\n") {
		out = append(out, ansi.Strip(line))
		if marks := highlightMarks(line); strings.Contains(marks, "^") {
			out = append(out, marks)
		}
	}
	compareGolden(t, name, strings.Join(out, "\n"), m.w)
}

// highlightMarks returns a row as wide as line with ^ under every cell drawn
// in the selection's style and spaces elsewhere.
func highlightMarks(line string) string {
	open := strings.TrimSuffix(selectionStyle.Render("x"), "x\x1b[m")
	var b strings.Builder
	on := false
	for line != "" {
		seq, width, n, _ := ansi.DecodeSequence(line, ansi.NormalState, nil)
		switch {
		case width > 0:
			mark := " "
			if on {
				mark = "^"
			}
			b.WriteString(strings.Repeat(mark, width))
		case strings.HasPrefix(seq, "\x1b["):
			on = seq == open
		}
		line = line[n:]
	}
	return b.String()
}

func TestSelectionGolden(t *testing.T) {
	m, _ := newPaneModel(t, "first line\nsecond line\nthird")
	dragMouse(m, [2]int{35, 2}, [2]int{33, 4})
	goldenSelection(t, "selection", m)
}

func TestSelectionClears(t *testing.T) {
	highlighted := func(m *Model) bool { return strings.Contains(highlightMarks(m.render()), "^") }
	for _, tc := range []struct {
		name  string
		after func(m *Model, sess *Session)
	}{
		{"on a key press", func(m *Model, _ *Session) { press(m, text("a")...) }},
		{"on a click", func(m *Model, _ *Session) { m.Update(click(40, 7, tea.MouseLeft)) }},
		{"on the wheel", func(m *Model, _ *Session) { m.Update(tea.MouseWheelMsg{X: 40, Y: 5, Button: tea.MouseWheelUp}) }},
		{"when the pane's output changes", func(m *Model, sess *Session) {
			r := m.sessions.runner(sess)
			r.term.Write([]byte("more"))
			r.activity.Store(true)
			m.Update(redrawMsg{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, sess := newPaneModel(t, "first line\nsecond line")
			dragMouse(m, [2]int{29, 2}, [2]int{33, 3})
			if !highlighted(m) {
				t.Fatal("the drag should leave a highlighted selection")
			}
			tc.after(m, sess)
			if highlighted(m) {
				t.Fatalf("selection still highlighted:\n%s", screen(m))
			}
		})
	}
}

func TestSelectionStaysWhenAnotherSessionWrites(t *testing.T) {
	m, _ := newPaneModel(t, "first line")
	dragMouse(m, [2]int{29, 2}, [2]int{33, 2})
	w, h := m.paneSize()
	other := &runner{term: newVterm(w, h, 0, nil)}
	other.activity.Store(true)
	m.sessions.runners[&Session{}] = other
	m.Update(redrawMsg{})
	if m.mode.sel == nil {
		t.Fatal("output in another Session must not clear the selection")
	}
}

func TestFullScreenProgramKeepsTheMouse(t *testing.T) {
	m, _ := newPaneModel(t, "\x1b[?1049hvim")
	if cmd := dragMouse(m, [2]int{29, 2}, [2]int{33, 3}); cmd != nil || m.mode.sel != nil {
		t.Fatal("a drag over a full-screen program goes to the program, not to a selection")
	}
}

// rightClick right-clicks the pane and delivers every message that follows
// within a short wait, as the Bubble Tea loop would.
func rightClick(m *Model) {
	_, cmd := m.Update(click(40, 4, tea.MouseRight))
	for _, msg := range cmdMsgs(cmd) {
		_, next := m.Update(msg)
		for _, msg := range cmdMsgs(next) {
			m.Update(msg)
		}
	}
}

func TestRightClickPastesTheClipboard(t *testing.T) {
	m, _, in := paneModel(t, Options{ReadClipboard: func() (string, error) { return "from os", nil }}, "$ ")
	rightClick(m)
	in.waitFor(t, "from os")
}

func TestRightClickFallsBackToTheTerminalClipboard(t *testing.T) {
	m, _, in := paneModel(t, Options{ReadClipboard: func() (string, error) { return "", errors.ErrUnsupported }}, "$ ")
	rightClick(m)
	m.Update(tea.ClipboardMsg{Content: "from osc 52"})
	in.waitFor(t, "from osc 52")
}

func TestRightClickHintsWhenTheClipboardCannotBeRead(t *testing.T) {
	defer func(w time.Duration) { clipboardWait = w }(clipboardWait)
	clipboardWait = 10 * time.Millisecond
	m, _, _ := paneModel(t, Options{}, "$ ")
	rightClick(m)
	if !strings.Contains(m.status, "Ctrl+Shift+V") {
		t.Fatalf("status = %q, want a hint naming Ctrl+Shift+V", m.status)
	}
}

func TestRightClickHintsAtOnceWhenTheClipboardHasNoText(t *testing.T) {
	m, _, _ := paneModel(t, Options{ReadClipboard: func() (string, error) { return "", nil }}, "$ ")
	rightClick(m) // waits far less than clipboardWait
	if !strings.Contains(m.status, "Ctrl+Shift+V") {
		t.Fatalf("status = %q, want a hint naming Ctrl+Shift+V", m.status)
	}
}

// doubleClick clicks the left button twice at x, y, as quickly as a user
// would, and returns what the second click asks for.
func doubleClick(m *Model, x, y int) tea.Cmd {
	m.Update(click(x, y, tea.MouseLeft))
	m.Update(release(x, y))
	_, cmd := m.Update(click(x, y, tea.MouseLeft))
	m.Update(release(x, y))
	return cmd
}

func TestDoubleClickCopiesTheWord(t *testing.T) {
	m, _ := newPaneModel(t, "ssh deploy@web01:/srv now")
	cmd := doubleClick(m, 29+8, 2)
	got, ok := clipboardSet(cmd)
	if !ok || got != "deploy@web01:/srv" {
		t.Fatalf("clipboard = %q, %v", got, ok)
	}
	if m.status != "copied 17 characters" {
		t.Fatalf("status = %q", m.status)
	}
	if !strings.Contains(highlightMarks(m.render()), "^^^^^^^^^^^^^^^^^") {
		t.Fatalf("the word should stay highlighted:\n%s", screen(m))
	}
}

func TestSlowSecondClickIsNoDoubleClick(t *testing.T) {
	defer func(d time.Duration) { doubleClickTime = d }(doubleClickTime)
	doubleClickTime = 0
	m, _ := newPaneModel(t, "ssh deploy@web01:/srv now")
	if cmd := doubleClick(m, 29+8, 2); cmd != nil || m.mode.sel != nil && !m.mode.sel.empty() {
		t.Fatal("two clicks further apart than doubleClickTime select nothing")
	}
}

func TestDoubleClickOnABlankSelectsNothing(t *testing.T) {
	m, _ := newPaneModel(t, "a    b")
	if cmd := doubleClick(m, 29+3, 2); cmd != nil {
		t.Fatal("a double-click on a blank copies nothing")
	}
}
