package workspace

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// groupEntries has the web group alpha, beta, delta and the orphaned gone,
// the ungrouped db and the batch group other.
var groupEntries = []Entry{
	{Alias: "alpha", Group: "web"},
	{Alias: "beta", Group: "web"},
	{Alias: "db", Description: "no group"},
	{Alias: "delta", Group: "web"},
	{Alias: "gone", Group: "web", Orphaned: true},
	{Alias: "other", Group: "batch"},
}

var keyG = tea.KeyPressMsg{Code: 'g', Text: "g"}

func TestPrefixGReportsEveryMemberThatFailsToStart(t *testing.T) {
	tests := []struct {
		name       string
		fail       []string
		wantOpen   []string
		wantStatus string
	}{
		{"one of three", []string{"beta"}, []string{"alpha", "delta"},
			"opened 2 connections in group web; could not open beta: no route to host"},
		{"two of three", []string{"alpha", "delta"}, []string{"beta"},
			"opened 1 connection in group web; could not open alpha, delta: no route to host"},
		{"all", []string{"alpha", "beta", "delta"}, nil,
			"could not open alpha, beta, delta in group web: no route to host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, r := newRecordingModel(t, groupEntries, tt.fail...)
			m.st.SetCursor("alpha")
			press(m, keyPrefix, keyG)
			if want := []string{"alpha", "beta", "delta"}; !slices.Equal(r.started(), want) {
				t.Errorf("sessions started for %v, want %v", r.started(), want)
			}
			var open []string
			for _, alias := range []string{"alpha", "beta", "delta"} {
				if m.st.Get(alias).Open() {
					open = append(open, alias)
				}
			}
			if !slices.Equal(open, tt.wantOpen) {
				t.Errorf("open = %v, want %v", open, tt.wantOpen)
			}
			if !m.statusErr || m.status != tt.wantStatus {
				t.Errorf("status = %q (err %v), want %q", m.status, m.statusErr, tt.wantStatus)
			}
		})
	}
}

func TestPrefixGOpensClosedGroupMembers(t *testing.T) {
	m, r := newRecordingModel(t, groupEntries)
	m.st.SetCursor("alpha")
	press(m, keyEnter) // alpha is already open before the group opens
	press(m, keyPrefix, keyG)

	if want := []string{"alpha", "beta", "delta"}; !slices.Equal(r.started(), want) {
		t.Errorf("sessions started for %v, want %v", r.started(), want)
	}
	if want := []string{"alpha", "beta", "delta"}; !slices.Equal(r.opened, want) {
		t.Errorf("history recorded %v, want %v", r.opened, want)
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
	m, r := newRecordingModel(t, groupEntries)
	press(m, text("/alpha")...)
	press(m, keyEnter) // stop typing the filter; the cursor stays on alpha
	press(m, keyPrefix, keyG)
	if want := []string{"alpha", "beta", "delta"}; !slices.Equal(r.started(), want) {
		t.Errorf("sessions started for %v, want %v", r.started(), want)
	}
}

func TestPrefixGWithNothingToOpenChangesNothing(t *testing.T) {
	tests := []struct {
		name  string
		setup func(m *Model)
	}{
		{"no group", func(m *Model) { m.st.SetCursor("db") }},
		{"all members open", func(m *Model) {
			m.st.SetCursor("other")
			press(m, keyEnter)
			press(m, keyPrefix, tea.KeyPressMsg{Code: tea.KeyTab}) // focus back to the list
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, r := newRecordingModel(t, groupEntries)
			tt.setup(m)
			before := r.started()
			active := m.st.Active()
			press(m, keyPrefix, keyG)
			if !slices.Equal(r.started(), before) {
				t.Errorf("sessions started for %v, want none beyond %v", r.started(), before)
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
