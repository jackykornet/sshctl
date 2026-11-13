package workspace

import (
	"errors"
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type commandCall struct {
	alias  string
	claude bool
}

// recorder records what a Workspace asks of its Options.Command and
// Options.OnOpen seams.
type recorder struct {
	calls  []commandCall
	opened []string
}

// started returns the aliases passed to Command, in call order.
func (r *recorder) started() []string {
	var out []string
	for _, c := range r.calls {
		out = append(out, c.alias)
	}
	return out
}

// newRecordingModel returns an 80x12 Workspace over entries whose fake
// Command records each call and runs the test binary with no tests, a process
// that exits at once on every platform. Command fails for the aliases in fail.
func newRecordingModel(t *testing.T, entries []Entry, fail ...string) (*Model, *recorder) {
	t.Helper()
	r := &recorder{}
	m, err := New(Options{
		Load: func() (Snapshot, error) { return Snapshot{Entries: entries}, nil },
		Command: func(alias string, claude bool) ([]string, error) {
			r.calls = append(r.calls, commandCall{alias, claude})
			for _, f := range fail {
				if f == alias {
					return nil, errors.New("no route to host")
				}
			}
			return []string{os.Args[0], "-test.run=^$"}, nil
		},
		OnOpen: func(alias string) error {
			r.opened = append(r.opened, alias)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return m, r
}
