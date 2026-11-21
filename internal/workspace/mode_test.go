package workspace

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// modeName names the Workspace's mode: list, filter or scroll, or a drag,
// selection or modal "over" the one it returns to.
func modeName(m *Model) string {
	names := map[modeKind]string{
		modeList: "list", modeFilter: "filter", modeScroll: "scroll",
		modeDrag: "drag", modeSelect: "select", modeModal: "modal",
	}
	if !m.mode.kind.over() {
		return names[m.mode.kind]
	}
	return names[m.mode.kind] + " over " + names[m.base().kind]
}

// A modeStep sends msg (or runs do) and expects the Workspace in mode want.
type modeStep struct {
	msg  tea.Msg
	do   func(m *Model, sess *Session)
	want string
}

func keyStep(k string, want string) modeStep {
	var msg tea.KeyPressMsg
	switch k {
	case "prefix":
		msg = keyPrefix
	case "enter":
		msg = keyEnter
	case "esc":
		msg = keyEsc
	case "down":
		msg = keyDown
	case "tab":
		msg = tea.KeyPressMsg{Code: tea.KeyTab}
	default:
		msg = text(k)[0]
	}
	return modeStep{msg: msg, want: want}
}

func wheel(b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: 40, Y: 5, Button: b}
}

// In the pane Workspace (newPaneModel) the pane body starts at column 29,
// row 1, and the list shows alpha on row 0 and beta on row 1, one row lower
// while the filter line shows.
func TestModeChanges(t *testing.T) {
	toList := []modeStep{keyStep("prefix", "list"), keyStep("tab", "list")}
	steps := func(groups ...[]modeStep) []modeStep {
		var out []modeStep
		for _, g := range groups {
			out = append(out, g...)
		}
		return out
	}
	tests := []struct {
		name  string
		steps []modeStep
	}{
		{"filter: / and Enter", steps(toList, []modeStep{keyStep("/", "filter"), keyStep("x", "filter"), keyStep("enter", "list")})},
		{"filter: / and Esc", steps(toList, []modeStep{keyStep("/", "filter"), keyStep("esc", "list")})},
		{"scroll: prefix s and q", []modeStep{keyStep("prefix", "list"), keyStep("s", "scroll"), keyStep("k", "scroll"), keyStep("q", "list")}},
		{"scroll: prefix s and Esc", []modeStep{keyStep("prefix", "list"), keyStep("s", "scroll"), keyStep("esc", "list")}},
		{"scroll: prefix s and G", []modeStep{keyStep("prefix", "list"), keyStep("s", "scroll"), keyStep("G", "list")}},
		{"scroll: wheel up and back down", []modeStep{
			{msg: wheel(tea.MouseWheelUp), want: "scroll"},
			{msg: wheel(tea.MouseWheelDown), want: "list"},
		}},
		{"scroll: another tab leaves it", []modeStep{keyStep("prefix", "list"), keyStep("s", "scroll"), keyStep("prefix", "scroll"), keyStep("]", "list")}},
		{"scroll: Session ended keeps it", []modeStep{
			keyStep("prefix", "list"), keyStep("s", "scroll"),
			{do: func(m *Model, sess *Session) { endSession(m, sess, 1) }, want: "scroll"},
		}},
		{"scroll: prefix s while filtering ends the filter", steps(toList, []modeStep{
			keyStep("/", "filter"), keyStep("prefix", "filter"), keyStep("s", "scroll"),
		})},
		{"filter: wheel keeps typing the filter", steps(toList, []modeStep{
			keyStep("/", "filter"),
			{msg: wheel(tea.MouseWheelUp), want: "filter"},
			keyStep("x", "filter"),
		})},
		{"select: drag in the pane, then a key", []modeStep{
			{msg: click(29, 1, tea.MouseLeft), want: "select over list"},
			{msg: motion(33, 2), want: "select over list"},
			{msg: release(33, 2), want: "select over list"},
			keyStep("x", "list"),
		}},
		{"select: a click elsewhere ends it", []modeStep{
			{msg: click(29, 1, tea.MouseLeft), want: "select over list"},
			{msg: release(33, 2), want: "select over list"},
			{msg: click(2, 1, tea.MouseLeft), want: "drag over list"},
			{msg: release(2, 1), want: "list"},
		}},
		{"select: the wheel ends it", []modeStep{
			{msg: click(29, 1, tea.MouseLeft), want: "select over list"},
			{msg: release(33, 2), want: "select over list"},
			{msg: wheel(tea.MouseWheelUp), want: "scroll"},
		}},
		{"select: in scroll mode returns to it", []modeStep{
			{msg: wheel(tea.MouseWheelUp), want: "scroll"},
			{msg: click(29, 1, tea.MouseLeft), want: "select over scroll"},
			{msg: release(33, 2), want: "select over scroll"},
			keyStep("k", "scroll"),
		}},
		{"select: Session ended keeps it", []modeStep{
			{msg: click(29, 1, tea.MouseLeft), want: "select over list"},
			{msg: release(33, 2), want: "select over list"},
			{do: func(m *Model, sess *Session) { endSession(m, sess, 1) }, want: "select over list"},
		}},
		{"drag: press and release in place", []modeStep{
			{msg: click(2, 1, tea.MouseLeft), want: "drag over list"},
			{msg: release(2, 1), want: "list"},
		}},
		{"drag: dropped off the list", []modeStep{
			{msg: click(2, 1, tea.MouseLeft), want: "drag over list"},
			{msg: motion(2, 3), want: "drag over list"},
			{msg: release(50, 3), want: "list"},
		}},
		{"drag: while filtering returns to it", steps(toList, []modeStep{
			keyStep("/", "filter"),
			{msg: click(2, 2, tea.MouseLeft), want: "drag over filter"},
			{msg: release(2, 2), want: "filter"},
		})},
		{"drag: a modal ends it", []modeStep{
			{msg: click(2, 1, tea.MouseLeft), want: "drag over list"},
			{msg: motion(2, 3), want: "drag over list"},
			keyStep("prefix", "drag over list"), keyStep("m", "modal over list"),
			keyStep("esc", "list"),
		}},
		{"modal: move prompt", []modeStep{keyStep("prefix", "list"), keyStep("m", "modal over list"), keyStep("esc", "list")}},
		{"modal: help closes on any key", []modeStep{keyStep("prefix", "list"), keyStep("?", "modal over list"), keyStep("down", "modal over list"), keyStep("x", "list")}},
		{"modal: confirmation answered no", []modeStep{keyStep("prefix", "list"), keyStep("w", "modal over list"), keyStep("n", "list")}},
		{"modal: over the filter returns to it", steps(toList, []modeStep{
			keyStep("/", "filter"), keyStep("prefix", "filter"), keyStep("m", "modal over filter"), keyStep("esc", "filter"),
		})},
		{"modal: over scroll mode returns to it", []modeStep{
			keyStep("prefix", "list"), keyStep("s", "scroll"), keyStep("prefix", "scroll"), keyStep("m", "modal over scroll"), keyStep("esc", "scroll"),
		}},
		{"modal: the picker and its prompt", steps(toList, []modeStep{
			keyStep("P", "modal over list"), keyStep("a", "modal over list"), keyStep("esc", "modal over list"), keyStep("esc", "list"),
		})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out []string
			for i := 1; i <= 40; i++ {
				out = append(out, fmt.Sprintf("line %d", i))
			}
			m, sess := newPaneModel(t, strings.Join(out, "\n")+"\n")
			if got := modeName(m); got != "list" {
				t.Fatalf("starts in %q, want list", got)
			}
			for i, s := range tt.steps {
				if s.do != nil {
					s.do(m, sess)
				} else {
					m.Update(s.msg)
				}
				if got := modeName(m); got != s.want {
					t.Fatalf("step %d (%v): mode %q, want %q", i+1, s.msg, got, s.want)
				}
			}
		})
	}
}
