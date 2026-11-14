package workspace

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newClaudeModel returns a recording Workspace over alpha, beta and the
// orphaned gone.
func newClaudeModel(t *testing.T) (*Model, *recorder) {
	t.Helper()
	return newRecordingModel(t, []Entry{{Alias: "alpha"}, {Alias: "beta"}, {Alias: "gone", Orphaned: true}})
}

func TestPrefixCOpensClaudeSessionOnCursorConnection(t *testing.T) {
	m, r := newClaudeModel(t)
	press(m, keyPrefix, text("c")[0])
	want := commandCall{"alpha", true}
	if len(r.calls) != 1 || r.calls[0] != want {
		t.Fatalf("Command calls = %v, want [%v]", r.calls, want)
	}
	if c := m.st.Active(); c == nil || c.Alias != "alpha" || len(c.Sessions) != 1 {
		t.Fatalf("active = %v", c)
	}
}

func TestTabBarMarksClaudeSession(t *testing.T) {
	m, _ := newClaudeModel(t)
	press(m, keyEnter)                // alpha 1: plain ssh
	press(m, keyPrefix, text("c")[0]) // alpha 2: claude on the Active connection
	bar, _ := m.tabBar(60)
	bar = ansi.Strip(bar)
	if !strings.Contains(bar, " alpha 1 ") || strings.Contains(bar, "alpha 1 claude") {
		t.Errorf("plain tab should not be marked: %q", bar)
	}
	if !strings.Contains(bar, " alpha 2 claude ") {
		t.Errorf("claude tab should be marked: %q", bar)
	}
}

func TestTabBarCombinesClaudeMarkerWithSessionState(t *testing.T) {
	m, _ := newClaudeModel(t)
	prefixC := []tea.KeyPressMsg{keyPrefix, text("c")[0]}
	press(m, prefixC...)              // alpha 1: will end
	press(m, prefixC...)              // alpha 2: will have unseen activity
	press(m, keyPrefix, text("n")[0]) // alpha 3: plain ssh, the shown tab
	c := m.st.Active()
	endSession(m, c.Sessions[0], 255)
	m.st.MarkActivity(c.Sessions[1])
	bar, spans := m.tabBar(80)
	want := " alpha 1 claude ✕  alpha 2 claude •  alpha 3 "
	if got := strings.TrimRight(ansi.Strip(bar), " "); got != strings.TrimRight(want, " ") {
		t.Errorf("tab bar = %q, want %q", got, want)
	}
	if len(spans) != 3 {
		t.Fatalf("spans = %v, want 3 tabs", spans)
	}
}

func TestReconnectKeepsClaudeKind(t *testing.T) {
	m, r := newClaudeModel(t)
	press(m, keyPrefix, text("c")[0])
	sess := m.st.Active().CurrentSession()
	pump(t, m, func() bool { return sess.Ended })
	press(m, text("r")...)
	if sess.Ended {
		t.Fatal("r should reconnect the ended tab")
	}
	want := []commandCall{{"alpha", true}, {"alpha", true}}
	if len(r.calls) != 2 || r.calls[0] != want[0] || r.calls[1] != want[1] {
		t.Fatalf("Command calls = %v, want %v", r.calls, want)
	}
	if bar, _ := m.tabBar(60); !strings.Contains(ansi.Strip(bar), "alpha 1 claude") {
		t.Fatalf("reconnected tab lost its marker: %q", ansi.Strip(bar))
	}
}

func TestClaudeSessionRecordsHistoryOnlyWhenFirst(t *testing.T) {
	prefixC := []tea.KeyPressMsg{keyPrefix, text("c")[0]}
	prefixN := []tea.KeyPressMsg{keyPrefix, text("n")[0]}
	tests := []struct {
		name string
		keys []tea.KeyPressMsg
	}{
		{"claude first", prefixC},
		{"claude after ssh", append([]tea.KeyPressMsg{keyEnter}, prefixC...)},
		{"ssh after claude", append(append([]tea.KeyPressMsg{}, prefixC...), prefixN...)},
		{"two claude tabs", append(append([]tea.KeyPressMsg{}, prefixC...), prefixC...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, r := newClaudeModel(t)
			press(m, tt.keys...)
			if len(r.opened) != 1 || r.opened[0] != "alpha" {
				t.Fatalf("OnOpen calls = %v, want [alpha]", r.opened)
			}
		})
	}
}

func TestOrphanedConnectionRefusesClaudeSession(t *testing.T) {
	m, r := newClaudeModel(t)
	press(m, keyDown, keyDown) // gone
	press(m, keyPrefix, text("c")[0])
	if len(r.calls) != 0 || m.st.Active() != nil {
		t.Fatalf("calls = %v active = %v", r.calls, m.st.Active())
	}
	if !m.statusErr || m.status != "gone is missing from ssh_config" {
		t.Fatalf("status = %q", m.status)
	}
}
