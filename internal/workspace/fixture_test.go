package workspace

import (
	"errors"
	"fmt"
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
// (with identity and startCollapsed) or fails with loadErr. It has the
// default Space, then spaces, with activeSpace active and spaceGroups as
// their Groups; SetActiveSpace records and changes activeSpace. It records every
// call, fails SetGroup while failGroup is set, fails each Save with the next
// error in saveErrs, and otherwise lists the saved Spec as it is (or drops
// the deleted alias) on the next Snapshot. Snapshot reports include, the
// Include ssh_config lacks, until AddInclude is called. ProvisionRepo fails
// with provision[alias], or returns a repo. The catalog's own rules are
// tested in internal/catalog.
type fakeCatalog struct {
	entries        []Entry
	groups         []string // the default Space's stored Group names, in order
	spaces         []Space  // the Spaces other than the default Space, in order
	defaultName    string   // the default Space's name; "" reads as "default"
	defaultAt      int      // the default Space's index among all Spaces
	spaceGroups    []Group  // the other Spaces' Groups
	activeSpace    string
	identity       string
	spaceKeys      map[string]string // SpaceIdentities
	startCollapsed []string
	loadErr        error
	failGroup      bool
	include        *IncludeError
	provision      map[string]error // ProvisionRepo's error per alias
	// off lists the Experimental features Snapshot reports off; every
	// other one is on, so tests of Bootstrap and Provision need not turn
	// them on.
	off []string

	loads     int
	opened    []string    // RecordOpen aliases
	moves     [][2]string // SetGroup alias, group
	collapsed [][]string  // SetCollapsed groups
	saves     []string    // "old→alias", one per Save
	specs     []Spec
	deletes   []string
	includes  int
	saveErrs  []error

	groupMoves   [][2]any // MoveGroup name, position
	moveGroupErr error
	groupOps     []string // "add x", "rename x→y", "delete x", one per Group call
	groupErr     error    // returned by the next Group call, which then changes nothing
	groupSpaces  []string // the Space of each Group call
	activeSaves  []string // SetActiveSpace spaces
	spaceOps     []string // "add x", "rename x→y", "delete x", "move x 2", one per Space call
	spaceErr     error    // returned by the next Space call, which then changes nothing
}

// spaceOp records op and returns spaceErr, once.
func (f *fakeCatalog) spaceOp(op string) error {
	f.spaceOps = append(f.spaceOps, op)
	err := f.spaceErr
	f.spaceErr = nil
	return err
}

// allSpaces returns every Space in order, the default Space among them.
func (f *fakeCatalog) allSpaces() []Space {
	name := f.defaultName
	if name == "" {
		name = "default"
	}
	return slices.Insert(slices.Clone(f.spaces), f.defaultAt, Space{Name: name, Default: true})
}

// setSpaces stores all, every Space in order, as spaces and defaultAt.
func (f *fakeCatalog) setSpaces(all []Space) {
	f.spaces = nil
	for i, s := range all {
		if s.Default {
			f.defaultName, f.defaultAt = s.Name, i
		} else {
			f.spaces = append(f.spaces, s)
		}
	}
}

// spaceIndex returns the index in spaces of the Space name, or -1 (also for
// the default Space, which spaces does not hold).
func (f *fakeCatalog) spaceIndex(name string) int {
	return slices.IndexFunc(f.spaces, func(s Space) bool { return s.Name == name })
}

func (f *fakeCatalog) AddSpace(name string) error {
	if err := f.spaceOp("add " + name); err != nil {
		return err
	}
	f.spaces = append(f.spaces, Space{Name: name})
	return nil
}

// RenameSpace renames a Space after the default Space, moving its Groups,
// Connections and active state with it, as the catalog does.
func (f *fakeCatalog) RenameSpace(old, name string) error {
	if err := f.spaceOp("rename " + old + "→" + name); err != nil {
		return err
	}
	i := f.spaceIndex(old)
	if i < 0 {
		f.defaultName = name // Connections refer to the default Space as ""
		return nil
	}
	f.spaces[i].Name = name
	for j := range f.spaceGroups {
		if f.spaceGroups[j].Space == old {
			f.spaceGroups[j].Space = name
		}
	}
	for j := range f.entries {
		if f.entries[j].Space == old {
			f.entries[j].Space = name
		}
	}
	if f.activeSpace == old {
		f.activeSpace = name
	}
	return nil
}

func (f *fakeCatalog) DeleteSpace(name string) error {
	if err := f.spaceOp("delete " + name); err != nil {
		return err
	}
	if i := f.spaceIndex(name); i >= 0 {
		f.spaces = slices.Delete(f.spaces, i, i+1)
	}
	if f.activeSpace == name {
		f.activeSpace = ""
	}
	return nil
}

// MoveSpace puts the Space name, the default Space too, at position,
// counting from 1.
func (f *fakeCatalog) MoveSpace(name string, position int) error {
	if err := f.spaceOp(fmt.Sprintf("move %s %d", name, position)); err != nil {
		return err
	}
	all := f.allSpaces()
	i := slices.IndexFunc(all, func(s Space) bool { return s.Name == name })
	if i < 0 || position < 1 || position > len(all) {
		return errors.New("bad space move")
	}
	s := all[i]
	f.setSpaces(slices.Insert(slices.Delete(all, i, i+1), position-1, s))
	return nil
}

func (f *fakeCatalog) Snapshot() (Snapshot, error) {
	f.loads++
	snap := Snapshot{Entries: append([]Entry(nil), f.entries...), Identity: f.identity, SpaceIdentities: f.spaceKeys, Collapsed: f.startCollapsed, ActiveSpace: f.activeSpace}
	snap.Spaces = f.allSpaces()
	for _, feature := range []string{catalog.FeatureBootstrap, catalog.FeatureProvision} {
		if err := snap.Experimental.Set(feature, !slices.Contains(f.off, feature)); err != nil {
			panic(err)
		}
	}
	for _, g := range f.groups {
		snap.Groups = append(snap.Groups, catalog.Group{Name: g})
	}
	snap.Groups = append(snap.Groups, f.spaceGroups...)
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
	f.entries = append(f.entries, Entry{Alias: s.Alias, Description: s.Description, Space: s.Space, Group: s.Group, Managed: s.Managed, Host: s.Host})
	return nil
}

func (f *fakeCatalog) Delete(alias string) error {
	f.deletes = append(f.deletes, alias)
	f.entries = slices.DeleteFunc(f.entries, func(e Entry) bool { return e.Alias == alias })
	return nil
}

func (f *fakeCatalog) AddInclude() error { f.includes++; return nil }

func (f *fakeCatalog) SetActiveSpace(space string) error {
	f.activeSaves = append(f.activeSaves, space)
	f.activeSpace = space
	return nil
}

// MoveGroup records the call and, unless moveGroupErr is set, puts name at
// position (from 1) in groups.
func (f *fakeCatalog) MoveGroup(_, name string, position int) error {
	f.groupMoves = append(f.groupMoves, [2]any{name, position})
	if f.moveGroupErr != nil {
		return f.moveGroupErr
	}
	i := slices.Index(f.groups, name)
	if i < 0 || position < 1 || position > len(f.groups) {
		return errors.New("bad group move")
	}
	f.groups = slices.Insert(slices.Delete(f.groups, i, i+1), position-1, name)
	return nil
}

// groupOp records op on space's Group and returns groupErr, once.
func (f *fakeCatalog) groupOp(space, op string) error {
	f.groupOps = append(f.groupOps, op)
	f.groupSpaces = append(f.groupSpaces, space)
	err := f.groupErr
	f.groupErr = nil
	return err
}

func (f *fakeCatalog) AddGroup(space, name string) error {
	if err := f.groupOp(space, "add "+name); err != nil {
		return err
	}
	f.groups = append(f.groups, name)
	return nil
}

func (f *fakeCatalog) RenameGroup(space, old, name string) error {
	if err := f.groupOp(space, "rename "+old+"→"+name); err != nil {
		return err
	}
	f.regroup(old, name)
	return nil
}

func (f *fakeCatalog) DeleteGroup(space, name string) error {
	if err := f.groupOp(space, "delete "+name); err != nil {
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
