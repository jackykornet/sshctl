package workspace

import (
	"errors"
	"os"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type commandCall struct {
	alias  string
	claude bool
}

// fakeCatalog stands in for the Connection catalog. Snapshot lists entries
// (with identity and startCollapsed) or fails with loadErr. It records every
// call, fails SetGroup while failGroup is set, fails each Save with the next
// error in saveErrs, and otherwise lists the saved Spec as it is (or drops
// the deleted alias) on the next Snapshot. The catalog's own rules are
// tested in internal/catalog.
type fakeCatalog struct {
	entries        []Entry
	identity       string
	startCollapsed []string
	loadErr        error
	failGroup      bool

	loads     int
	opened    []string    // RecordOpen aliases
	moves     [][2]string // SetGroup alias, group
	collapsed [][]string  // SetCollapsed groups
	saves     []string    // "old→alias", one per Save
	specs     []Spec
	deletes   []string
	includes  int
	saveErrs  []error
}

func (f *fakeCatalog) Snapshot() (Snapshot, error) {
	f.loads++
	snap := Snapshot{Entries: append([]Entry(nil), f.entries...), Identity: f.identity, Collapsed: f.startCollapsed}
	return snap, f.loadErr
}

func (f *fakeCatalog) RecordOpen(alias string) error {
	f.opened = append(f.opened, alias)
	return nil
}

func (f *fakeCatalog) SetGroup(alias, group string) error {
	if f.failGroup {
		return errors.New("disk full")
	}
	f.moves = append(f.moves, [2]string{alias, group})
	return nil
}

func (f *fakeCatalog) SetCollapsed(groups []string) error {
	f.collapsed = append(f.collapsed, groups)
	return nil
}

func (f *fakeCatalog) Save(old string, s Spec) error {
	f.saves = append(f.saves, old+"→"+s.Alias)
	f.specs = append(f.specs, s)
	if len(f.saveErrs) > 0 {
		err := f.saveErrs[0]
		f.saveErrs = f.saveErrs[1:]
		if err != nil {
			return err
		}
	}
	f.entries = slices.DeleteFunc(f.entries, func(e Entry) bool { return e.Alias == old })
	f.entries = append(f.entries, Entry{Alias: s.Alias, Description: s.Description, Group: s.Group, Managed: s.Managed, Host: s.Host})
	return nil
}

func (f *fakeCatalog) Delete(alias string) error {
	f.deletes = append(f.deletes, alias)
	f.entries = slices.DeleteFunc(f.entries, func(e Entry) bool { return e.Alias == alias })
	return nil
}

func (f *fakeCatalog) AddInclude() error { f.includes++; return nil }

// recorder records what a Workspace asks of its Options.Command seam, and
// through its fakeCatalog which Connections were recorded as opened.
type recorder struct {
	calls []commandCall
	*fakeCatalog
}

// started returns the aliases passed to Command, in call order.
func (r *recorder) started() []string {
	var out []string
	for _, c := range r.calls {
		out = append(out, c.alias)
	}
	return out
}

// endSession delivers the message sess's runner sends when its process exits
// with code, as if the process had just ended.
func endSession(m *Model, sess *Session, code int) {
	m.Update(sessionEndedMsg{sess: sess, r: m.sessions.runner(sess), code: code})
}

// newRecordingModel returns an 80x12 Workspace over entries whose fake
// Command records each call and runs the test binary with no tests, a process
// that exits at once on every platform. Command fails for the aliases in fail.
func newRecordingModel(t *testing.T, entries []Entry, fail ...string) (*Model, *recorder) {
	t.Helper()
	r := &recorder{fakeCatalog: &fakeCatalog{entries: entries}}
	m, err := New(Options{
		Catalog: r.fakeCatalog,
		Command: func(alias string, claude bool) ([]string, error) {
			r.calls = append(r.calls, commandCall{alias, claude})
			for _, f := range fail {
				if f == alias {
					return nil, errors.New("no route to host")
				}
			}
			return []string{os.Args[0], "-test.run=^$"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.closeAll)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	return m, r
}
