package workspace

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// cursorGlyph marks where typed text goes in a line being edited.
const cursorGlyph = "█"

// line is a single line of text being edited, with a cursor. The connection
// form, the Group and move prompts and the filter all edit through it.
type line struct {
	text []rune
	// pos is the cursor, as the number of runes before it.
	pos int
}

// newLine returns a line holding s, with the cursor at its end.
func newLine(s string) line {
	r := []rune(s)
	return line{text: r, pos: len(r)}
}

func (l line) String() string { return string(l.text) }

func (l line) empty() bool { return len(l.text) == 0 }

// withCursor returns the text with the cursor glyph drawn where typed text
// would be inserted.
func (l line) withCursor() string {
	return string(l.text[:l.pos]) + cursorGlyph + string(l.text[l.pos:])
}

// end moves the cursor to the end of the line.
func (l *line) end() { l.pos = len(l.text) }

// key applies a line-editing key: ←/→ move one character, Home/End and
// Ctrl+A/Ctrl+E jump to the start or end, Backspace and Delete remove the
// character before or after the cursor, Ctrl+U clears the line, and
// printable text is inserted at the cursor. It reports false, leaving the
// line as it was, for any other key.
func (l *line) key(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "left":
		l.pos = max(0, l.pos-1)
	case "right":
		l.pos = min(len(l.text), l.pos+1)
	case "home", "ctrl+a":
		l.pos = 0
	case "end", "ctrl+e":
		l.end()
	case "backspace":
		if l.pos > 0 {
			l.text = append(l.text[:l.pos-1], l.text[l.pos:]...)
			l.pos--
		}
	case "delete":
		if l.pos < len(l.text) {
			l.text = append(l.text[:l.pos], l.text[l.pos+1:]...)
		}
	case "ctrl+u":
		l.text, l.pos = nil, 0
	default:
		if msg.Text == "" || msg.Mod&^tea.ModShift != 0 {
			return false
		}
		l.insert([]rune(msg.Text))
	}
	return true
}

// paste inserts pasted text at the cursor, dropping line breaks and other
// control characters, since the line holds a single line.
func (l *line) paste(text string) {
	var r []rune
	for _, c := range text {
		if !unicode.IsControl(c) {
			r = append(r, c)
		}
	}
	l.insert(r)
}

func (l *line) insert(r []rune) {
	text := make([]rune, 0, len(l.text)+len(r))
	text = append(text, l.text[:l.pos]...)
	text = append(text, r...)
	l.text = append(text, l.text[l.pos:]...)
	l.pos += len(r)
}
