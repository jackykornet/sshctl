package main

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/platform"
)

// The group commands take --space to pick the Space; without it, a Group is
// looked up in every Space (see findGroup). A Group is given by name or
// Group number, 0 for ungrouped; a name keeps scripts working when Groups
// are reordered.

func (a *app) groupAdd(space spaceOption, in input) error {
	args := in.args
	if len(args) != 1 {
		return errUsage
	}
	return a.catalog().AddGroup(space.name, args[0])
}

func (a *app) groupRename(space spaceOption, in input) error {
	args := in.args
	if len(args) != 2 {
		return errUsage
	}
	return a.withGroup(space, args[0], func(ref, name string) error {
		return a.catalog().RenameGroup(ref, name, args[1])
	})
}

func (a *app) groupMove(space spaceOption, in input) error {
	args := in.args
	if len(args) != 2 {
		return errUsage
	}
	position, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("position %q is not a number", args[1])
	}
	return a.withGroup(space, args[0], func(ref, name string) error {
		return a.catalog().MoveGroup(ref, name, position)
	})
}

// groupConnect opens a terminal window for each Connection of the Group
// arg, or with 0 each ungrouped Connection of the Space.
func (a *app) groupConnect(space spaceOption, in input) error {
	args := in.args
	if len(args) != 1 {
		return errUsage
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	ref, group, err := findGroup(snap, space, args[0])
	if err != nil {
		return err
	}
	var names []string
	for _, e := range snap.Entries {
		if e.Space == ref && e.Group == group {
			names = append(names, e.Alias)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		if group == "" {
			return fmt.Errorf("space %s has no ungrouped Connections", snap.SpaceName(ref))
		}
		return fmt.Errorf("group %q has no Connections", group)
	}
	open := a.openTerminal
	if open == nil {
		open = platform.OpenSSH
	}
	for _, n := range names {
		if err := open(n, snap.SpaceIdentity(ref)); err != nil {
			return err
		}
	}
	return nil
}

// withGroup finds the Group arg (see findGroup) and runs do with its Space,
// as Groups refer to it, and its name. It refuses 0: ungrouped is not a
// Group to change.
func (a *app) withGroup(space spaceOption, arg string, do func(ref, name string) error) error {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	ref, name, err := findStoredGroup(snap, space, arg)
	if err != nil {
		return err
	}
	return do(ref, name)
}

// findGroup returns the Space, as Groups refer to it, and the name of the
// Group arg ("" for 0, ungrouped): with --space the Space's Group
// (catalog.Snapshot.ResolveGroup), and without, the one Space's that has it
// (catalog.Snapshot.FindGroup).
func findGroup(snap catalog.Snapshot, space spaceOption, arg string) (ref, name string, err error) {
	if space.set {
		name, err := snap.ResolveGroup(space.ref, arg)
		return space.ref, name, err
	}
	ref, name, err = snap.FindGroup(arg)
	if errors.Is(err, catalog.ErrGroupInSeveralSpaces) {
		return "", "", fmt.Errorf("%w; pick one with --space", err)
	}
	return ref, name, err
}

// findStoredGroup is findGroup for a command that changes a Group, so it
// refuses 0.
func findStoredGroup(snap catalog.Snapshot, space spaceOption, arg string) (ref, name string, err error) {
	ref, name, err = findGroup(snap, space, arg)
	if err == nil && name == "" {
		return "", "", errors.New("0 stands for ungrouped, which is not a Group; give a Group name or number from 1")
	}
	return ref, name, err
}

// groupList prints each Group's number, name, member count, dotfiles
// setting and Indicator color, if any, Space by Space. With --space it
// lists that Space's Groups; with
// several Spaces and no --space, each line starts with the Space's name.
func (a *app) groupList(space spaceOption, in input) error {
	args := in.args
	if len(args) != 0 {
		return errUsage
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	spaces := space.spaces(snap)
	records := map[groupKey]catalog.Group{}
	for _, g := range snap.Groups {
		records[groupKey{g.Space, g.Name}] = g
	}
	members := memberCounts(snap)
	if in.flag("json") {
		groups := []groupJSON{}
		for _, s := range spaces {
			for i, name := range snap.GroupNames(s.ref) {
				k := groupKey{s.ref, name}
				groups = append(groups, groupJSON{Space: s.name, Number: i + 1, Name: name, Connections: members[k], Dotfiles: records[k].Dotfiles, Color: records[k].Color})
			}
		}
		return a.writeJSON(groups)
	}
	for _, s := range spaces {
		prefix := ""
		if len(spaces) > 1 {
			prefix = fmt.Sprintf("%-20s ", s.name)
		}
		for i, name := range snap.GroupNames(s.ref) {
			k := groupKey{s.ref, name}
			g := records[k]
			fmt.Fprintf(a.out, "%s%-2d %-20s %-2d %s%s\n", prefix, i+1, name, members[k], describeDotfiles(g.Dotfiles), colorTag(g.Color))
		}
	}
	return nil
}

// groupDelete deletes the Group args name (see findGroup) after asking,
// unless --yes is given. Its Connections become ungrouped; none is deleted.
func (a *app) groupDelete(space spaceOption, in input) error {
	if len(in.args) != 1 {
		return errUsage
	}
	arg, yes := in.args[0], in.flag("yes")
	cat := a.catalog()
	snap, err := cat.Snapshot()
	if err != nil {
		return err
	}
	ref, name, err := findStoredGroup(snap, space, arg)
	if err != nil {
		return err
	}
	if !yes {
		n := memberCounts(snap)[groupKey{ref, name}]
		noun, verb := "Connections", "become"
		if n == 1 {
			noun, verb = "Connection", "becomes"
		}
		fmt.Fprintf(a.out, "Delete group %s? %d %s %s ungrouped. [y/N] ", name, n, noun, verb)
		if !confirm(a.in) {
			return nil
		}
	}
	return cat.DeleteGroup(ref, name)
}

// groupKey is a Group: its Space, as Groups refer to it, and its name.
type groupKey struct{ space, name string }

// memberCounts counts each Group's Connections, Orphaned ones included.
func memberCounts(snap catalog.Snapshot) map[groupKey]int {
	members := map[groupKey]int{}
	for _, e := range snap.Entries {
		members[groupKey{e.Space, e.Group}]++
	}
	return members
}
