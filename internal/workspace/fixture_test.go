package workspace

import (
	"errors"
	"os"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sshctl/sshctl/internal/catalog"
)

type commandCall struct {
	alias  string
	launch Launch
}

// fakeCatalog stands in for the Connection catalog. Snapshot lists entries
// (with identity and startCollapsed) or fails with loadErr. It records every
// call, fails SetGroup while failGroup is set, fails each Save with the next
// error in saveErrs, and otherwise lists the saved Spec as it is (or drops
// the deleted alias) on the next Snapshot. Snapshot reports include, the
// Include ssh_config lacks, until AddInclude is called. ProvisionRepo fails
// with provision[alias], or returns a repo. The catalog's own rules are
// tested in internal/catalog.
type fakeCatalog struct {
	entries        []Entry
	groups         []string // stored Group names, in order
	identity       string
	startCollapsed []string
	loadErr        error
	failGroup      bool
	include        *IncludeError
	provision      map[string]error // ProvisionRepo's error per alias

	loads     int
	opened    []string    // RecordOpen aliases
	moves     [][2]string // SetGroup alias, group
	collapsed [][]string  // SetCollapsed groups
	saves     []string    // "old→alias", one per Save
	specs     []Spec
	deletes   []string
	includes  int
	saveErrs  []error
	groupOps  []string // "add x", "rename x→y", "delete x", one per Group call
	groupErr  error    // returned by the next Group call, which then changes nothing
}

func (f *fakeCatalog) Snapshot() (Snapshot, error) {
	f.loads++
	snap := Snapshot{Entries: append([]Entry(nil), f.entries...), Identity: f.identity, Collapsed: f.startCollapsed}
	for _, g := range f.groups {
		snap.Groups = append(snap.Groups, catalog.Group{Name: g})
	}
	if f.includes == 0 {
		snap.Include = f.include
	}
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

// groupOp records op and returns groupErr, once.
func (f *fakeCatalog) groupOp(op string) error {
	f.groupOps = append(f.groupOps, op)
	err := f.groupErr
	f.groupErr = nil
	return err
}

func (f *fakeCatalog) AddGroup(name string) error {
	if err := f.groupOp("add " + name); err != nil {
		return err
	}
	f.groups = append(f.groups, name)
	return nil
}

func (f *fakeCatalog) RenameGroup(old, name string) error {
	if err := f.groupOp("rename " + old + "→" + name); err != nil {
		return err
	}
	f.regroup(old, name)
	return nil
}

func (f *fakeCatalog) DeleteGroup(name string) error {
	if err := f.groupOp("delete " + name); err != nil {
		return err
	}
	f.regroup(name, "")
	return nil
}

// regroup renames the Group old to name, or deletes it when name is "",
// moving its members with it.
func (f *fakeCatalog) regroup(old, name string) {
	if i := slices.Index(f.groups, old); i >= 0 {
		if name == "" {
			f.groups = slices.Delete(f.groups, i, i+1)
		} else {
			f.groups[i] = name
		}
	}
	for i := range f.entries {
		if f.entries[i].Group == old {
			f.entries[i].Group = name
		}
	}
}

func (f *fakeCatalog) ProvisionRepo(alias string) (string, error) {
	if err := f.provision[alias]; err != nil {
		return "", err
	}
	return "https://example.com/dotfiles.git", nil
}

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
		Command: func(alias string, l Launch) ([]string, error) {
			r.calls = append(r.calls, commandCall{alias, l})
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
