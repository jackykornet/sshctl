package workspace

import (
	"bytes"
	"encoding/base64"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// vterm is the terminal emulator behind one Session's pane. It is the only
// code that depends on charmbracelet/x/vt (see docs/adr/0001), so the
// emulator can be swapped without touching the rest of the Workspace.
type vterm struct {
	em *vt.SafeEmulator
}

// newVterm creates an emulator of w×h cells. onClipboard receives text that
// a remote program copies with OSC 52.
func newVterm(w, h, scrollback int, onClipboard func(string)) *vterm {
	em := vt.NewSafeEmulator(w, h)
	em.SetScrollbackSize(scrollback)
	em.RegisterOscHandler(52, func(data []byte) bool {
		if text, ok := parseOSC52(data); ok && onClipboard != nil {
			onClipboard(text)
		}
		return true
	})
	return &vterm{em: em}
}

// parseOSC52 decodes "52;<selection>;<base64>" (the "52;" may be absent).
// Queries ("?") are refused so remote programs cannot read the clipboard.
func parseOSC52(data []byte) (string, bool) {
	parts := bytes.Split(data, []byte{';'})
	payload := parts[len(parts)-1]
	if len(parts) < 2 || string(payload) == "?" {
		return "", false
	}
	b, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Write feeds program output into the emulator.
func (t *vterm) Write(p []byte) (int, error) { return t.em.Write(p) }

// Read returns bytes the emulator sends to the program: encoded key presses,
// mouse events, pastes and replies to terminal queries.
func (t *vterm) Read(p []byte) (int, error) { return t.em.Read(p) }

// Close stops Read, which ends the goroutine forwarding input to the program.
// It closes only the emulator's input pipe: vt's own Close is not safe to
// call while output is still being written.
func (t *vterm) Close() error {
	if c, ok := t.em.InputPipe().(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (t *vterm) Resize(w, h int) { t.em.Resize(w, h) }

func (t *vterm) Size() (int, int) { return t.em.Width(), t.em.Height() }

func (t *vterm) SendKey(k tea.KeyPressMsg) { t.em.SendKey(uv.KeyPressEvent(k)) }

func (t *vterm) Paste(s string) { t.em.Paste(s) }

// SendMouse forwards a mouse event (pane coordinates) if the program enabled mouse reporting.
func (t *vterm) SendMouse(msg tea.MouseMsg, x, y int) {
	m := msg.Mouse()
	m.X, m.Y = x, y
	switch msg.(type) {
	case tea.MouseClickMsg:
		t.em.SendMouse(uv.MouseClickEvent(m))
	case tea.MouseReleaseMsg:
		t.em.SendMouse(uv.MouseReleaseEvent(m))
	case tea.MouseWheelMsg:
		t.em.SendMouse(uv.MouseWheelEvent(m))
	case tea.MouseMotionMsg:
		t.em.SendMouse(uv.MouseMotionEvent(m))
	}
}

// AltScreen reports whether a full-screen program (vim, htop) is running.
func (t *vterm) AltScreen() bool { return t.em.IsAltScreen() }

func (t *vterm) ScrollbackLen() int { return t.em.ScrollbackLen() }

// Cursor returns the cursor position and whether it should be shown.
func (t *vterm) Cursor() (x, y int, visible bool) {
	p := t.em.CursorPosition()
	return p.X, p.Y, !t.em.CursorHidden()
}

// Text returns the plain text of every cell from from to to (inclusive, in
// reading order) as Lines(scroll) shows them. Each row's trailing blanks are
// trimmed and rows are joined with newlines. vt does not record which rows
// were wrapped, so a wrapped line is split too.
func (t *vterm) Text(scroll int, from, to pos) string {
	w, _ := t.Size()
	n := t.em.ScrollbackLen()
	scroll = max(0, min(scroll, n))
	cell := func(x, y int) *uv.Cell {
		if y < scroll {
			return t.em.ScrollbackCellAt(x, n-scroll+y)
		}
		return t.em.CellAt(x, y-scroll)
	}
	rows := make([]string, 0, to.y-from.y+1)
	for y := from.y; y <= to.y; y++ {
		x0, x1 := 0, w-1
		if y == from.y {
			x0 = from.x
		}
		if y == to.y {
			x1 = min(to.x, w-1)
		}
		var b strings.Builder
		for x := x0; x <= x1; x++ {
			c := cell(x, y)
			switch {
			case c == nil:
				b.WriteByte(' ') // past the end of a history line
			case c.Content == "" && c.Width == 0:
				// the second column of a wide character
			default:
				b.WriteString(c.Content)
			}
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}
	return strings.Join(rows, "\n")
}

// Lines renders the screen as styled lines. With scroll > 0 the view moves
// that many lines back into the scrollback.
func (t *vterm) Lines(scroll int) []string {
	screen := strings.Split(t.em.Render(), "\n")
	_, h := t.Size()
	if len(screen) > h {
		screen = screen[:h]
	}
	if scroll <= 0 {
		return screen
	}
	sb := t.em.Scrollback()
	n := sb.Len()
	scroll = min(scroll, n)
	lines := make([]string, 0, h)
	for i := n - scroll; i < n && len(lines) < h; i++ {
		lines = append(lines, sb.Line(i).Render())
	}
	for i := 0; len(lines) < h && i < len(screen); i++ {
		lines = append(lines, screen[i])
	}
	return lines
}
