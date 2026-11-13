package workspace

import (
	"os"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newGroupModel returns a Workspace whose fake Command records each alias and
// runs the test binary with no tests, a process that exits at once on every
// platform. It also returns the aliases passed to Command and to OnOpen.
func newGroupModel(t *testing.T) (m *Model, started, opened *[]string) {
	t.Helper()
	started, opened = &[]string{}, &[]string{}
	m, err := New(Options{
		Load: func() (Snapshot, error) {
			return Snapshot{Entries: []Entry{
				{Alias: "alpha", Group: "web"},
				{Alias: "beta", Group: "web"},
				{Alias: "db", Description: "no group"},
				{Alias: "delta", Group: "web"},
				{Alias: "gone", Group: "web", Orphaned: true},
				{Alias: "other", Group: "batch"},
			}}, nil
		},
		Command: func(alias string, _ bool) ([]string, error) {
			*started = append(*started, alias)
			return []string{os.Args[0], "-test.run=^$"}, nil
		},
		OnOpen: func(alias string) error {
			*opened = append(*opened, alias)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return m, started, opened
}

var keyG = tea.KeyPressMsg{Code: 'g', Text: "g"}

func TestPrefixGOpensClosedGroupMembers(t *testing.T) {
	m, started, opened := newGroupModel(t)
	press(m, keyEnter) // alpha is already open before the group opens
	press(m, keyPrefix, keyG)

	if want := []string{"alpha", "beta", "delta"}; !slices.Equal(*started, want) {
		t.Errorf("sessions started for %v, want %v", *started, want)
	}
	if want := []string{"alpha", "beta", "delta"}; !slices.Equal(*opened, want) {
		t.Errorf("history recorded %v, want %v", *opened, want)
	}
	if got := len(m.st.Get("alpha").Sessions); got != 1 {
		t.Errorf("alpha has %d sessions, want 1 (already open)", got)
	}
	if m.st.Get("gone").Open() || m.st.Get("other").Open() || m.st.Get("db").Open() {
		t.Error("orphaned and non-member connections should stay closed")
	}
	if a := m.st.Active(); a == nil || a.Alias != "beta" {
		t.Errorf("active = %v, want beta (first newly opened)", a)
	}
	if m.statusErr || m.status == "" {
		t.Errorf("status = %q (err %v), want a success message", m.status, m.statusErr)
	}
}

func TestPrefixGIgnoresListFilter(t *testing.T) {
	m, started, _ := newGroupModel(t)
	press(m, text("/alpha")...)
	press(m, keyEnter) // stop typing the filter; the cursor stays on alpha
	press(m, keyPrefix, keyG)
	if want := []string{"alpha", "beta", "delta"}; !slices.Equal(*started, want) {
		t.Errorf("sessions started for %v, want %v", *started, want)
	}
}

func TestPrefixGWithNothingToOpenChangesNothing(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *Model)
	}{
		{"no group", func(m *Model) { press(m, keyDown, keyDown) }}, // cursor on db
		{"all members open", func(m *Model) {
			m.st.SetCursor("other")
			press(m, keyEnter)
			press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}) // focus back to the list
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, started, _ := newGroupModel(t)
			tt.setup(m)
			before := slices.Clone(*started)
			active := m.st.Active()
			press(m, keyPrefix, keyG)
			if !slices.Equal(*started, before) {
				t.Errorf("sessions started for %v, want none beyond %v", *started, before)
			}
			if m.st.Active() != active {
				t.Errorf("active changed to %v", m.st.Active())
			}
			if m.status == "" {
				t.Error("want a status message")
			}
		})
	}
}
