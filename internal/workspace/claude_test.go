package workspace

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type commandCall struct {
	alias  string
	claude bool
}

// newClaudeModel returns a Workspace whose fake Command records each call and
// starts a process that exits at once (the test binary running no tests), so
// it works on every platform.
func newClaudeModel(t *testing.T) (*Model, *[]commandCall, *[]string) {
	t.Helper()
	var calls []commandCall
	var opened []string
	m, err := New(Options{
		Load: func() (Snapshot, error) {
			return Snapshot{Entries: []Entry{{Alias: "alpha"}, {Alias: "beta"}, {Alias: "gone", Orphaned: true}}}, nil
		},
		Command: func(alias string, claude bool) ([]string, error) {
			calls = append(calls, commandCall{alias, claude})
			return []string{os.Args[0], "-test.run=^$"}, nil
		},
		OnOpen: func(alias string) error {
			opened = append(opened, alias)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return m, &calls, &opened
}

func TestPrefixCOpensClaudeSessionOnCursorConnection(t *testing.T) {
	m, calls, _ := newClaudeModel(t)
	press(m, keyPrefix, text("c")[0])
	want := commandCall{"alpha", true}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("Command calls = %v, want [%v]", *calls, want)
	}
	if c := m.st.Active(); c == nil || c.Alias != "alpha" || len(c.Sessions) != 1 {
		t.Fatalf("active = %v", c)
	}
}

func TestTabBarMarksClaudeSession(t *testing.T) {
	m, _, _ := newClaudeModel(t)
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

func TestReconnectKeepsClaudeKind(t *testing.T) {
	m, calls, _ := newClaudeModel(t)
	press(m, keyPrefix, text("c")[0])
	sess := m.st.Active().CurrentSession()
	pump(t, m, func() bool { return sess.Ended })
	press(m, text("r")...)
	if sess.Ended {
		t.Fatal("r should reconnect the ended tab")
	}
	want := []commandCall{{"alpha", true}, {"alpha", true}}
	if len(*calls) != 2 || (*calls)[0] != want[0] || (*calls)[1] != want[1] {
		t.Fatalf("Command calls = %v, want %v", *calls, want)
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
			m, _, opened := newClaudeModel(t)
			press(m, tt.keys...)
			if len(*opened) != 1 || (*opened)[0] != "alpha" {
				t.Fatalf("OnOpen calls = %v, want [alpha]", *opened)
			}
		})
	}
}

func TestOrphanedConnectionRefusesClaudeSession(t *testing.T) {
	m, calls, _ := newClaudeModel(t)
	press(m, keyDown, keyDown) // gone
	press(m, keyPrefix, text("c")[0])
	if len(*calls) != 0 || m.st.Active() != nil {
		t.Fatalf("calls = %v active = %v", *calls, m.st.Active())
	}
	if !m.statusErr || m.status != "gone is missing from ssh_config" {
		t.Fatalf("status = %q", m.status)
	}
}
