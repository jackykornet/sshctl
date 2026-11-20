package workspace

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// A right-click in a live Session's pane pastes the system clipboard into it
// (ADR-0006). The clipboard comes from Options.ReadClipboard, or when that
// cannot read it, from the host terminal with OSC 52, which many terminals
// never answer.

// pendingPaste is a right-click paste waiting for the clipboard.
type pendingPaste struct {
	id   int
	sess *Session
}

// Messages of a right-click paste, for the paste with the same id.
type (
	// clipboardReadMsg carries what Options.ReadClipboard read; "" is a
	// clipboard holding no text.
	clipboardReadMsg struct {
		id   int
		text string
	}
	// clipboardMissMsg says Options.ReadClipboard could not read the clipboard.
	clipboardMissMsg struct{ id int }
	// clipboardTimeoutMsg says the host terminal did not answer in time.
	clipboardTimeoutMsg struct{ id int }
)

// clipboardWait is how long a right-click paste waits for the host terminal
// to answer an OSC 52 clipboard read.
var clipboardWait = time.Second

// clipboardHint is shown when a right-click paste finds no text to paste.
const clipboardHint = "could not read the clipboard; paste with Ctrl+Shift+V"

// startPaste starts a right-click paste into sess, replacing any still waiting.
func (m *Model) startPaste(sess *Session) tea.Cmd {
	m.pastes++
	m.rightPaste = &pendingPaste{id: m.pastes, sess: sess}
	id, read := m.pastes, m.opts.ReadClipboard
	return func() tea.Msg {
		if read != nil {
			if text, err := read(); err == nil {
				return clipboardReadMsg{id, text}
			}
		}
		return clipboardMissMsg{id}
	}
}

// clipboardRead handles the messages of a right-click paste, and reports
// whether msg was one.
func (m *Model) clipboardRead(msg tea.Msg) (tea.Cmd, bool) {
	p := m.rightPaste
	switch msg := msg.(type) {
	case clipboardReadMsg:
		if p != nil && p.id == msg.id {
			return m.endPaste(msg.text), true
		}
	case clipboardMissMsg:
		if p != nil && p.id == msg.id {
			return tea.Batch(tea.ReadClipboard, tea.Tick(clipboardWait, func(time.Time) tea.Msg {
				return clipboardTimeoutMsg{msg.id}
			})), true
		}
	case tea.ClipboardMsg:
		// An OSC 52 answer carries no id; it goes to the paste waiting now.
		if p != nil {
			return m.endPaste(msg.Content), true
		}
	case clipboardTimeoutMsg:
		if p != nil && p.id == msg.id {
			return m.endPaste(""), true
		}
	default:
		return nil, false
	}
	return nil, true
}

// endPaste ends the waiting paste: it pastes text into its Session if that
// is still live, or shows a hint when there is no text.
func (m *Model) endPaste(text string) tea.Cmd {
	sess := m.rightPaste.sess
	m.rightPaste = nil
	if text == "" {
		return m.setStatus(clipboardHint, true)
	}
	if r := m.sessions.runner(sess); r != nil && !sess.Ended {
		r.term.Paste(text)
	}
	return nil
}
