package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
)

const groupUsage = "group [--space <name>] list | add <name> | rename <group> <new> | delete <group> [--yes] | move <group> <position> | connect <group>"

// groupHelp is groupUsage with a note that group connect opens OS terminal
// windows (internal/platform), unlike prefix g in the Workspace, which opens
// tabs. Both `sshctl group --help` and a wrong group call print it.
const groupHelp = groupUsage + "\n\ngroup connect opens one terminal window per Connection in the Group;\nto open them as tabs, use prefix g in the Workspace (run sshctl with no arguments)."

// groupCommand runs `sshctl group ...`. --space picks the Space; without
// it, a Group is looked up in every Space (see findGroup). A Group is given
// by name or Group number; a name keeps scripts working when Groups are
// reordered.
func (a *app) groupCommand(args []string) error {
	space, args, err := cutSpaceOption(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("usage: sshctl " + groupHelp)
	}
	cat := a.catalog()
	switch args[0] {
	case "list":
		if len(args) == 1 {
			return a.groupList(space)
		}
	case "add":
		if len(args) == 2 {
			return cat.AddGroup(space.name, args[1])
		}
	case "rename":
		if len(args) == 3 {
			return a.withGroup(space, args[1], func(ref, name string) error {
				return cat.RenameGroup(ref, name, args[2])
			})
		}
	case "delete":
		var names []string
		yes := false
		for _, arg := range args[1:] {
			if arg == "--yes" {
				yes = true
			} else {
				names = append(names, arg)
			}
		}
		if len(names) == 1 {
			return a.groupDelete(space, names[0], yes)
		}
	case "move":
		if len(args) == 3 {
			position, err := strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("position %q is not a number", args[2])
			}
			return a.withGroup(space, args[1], func(ref, name string) error {
				return cat.MoveGroup(ref, name, position)
			})
		}
	case "connect":
		return a.groupConnect(space, args[1:])
	}
	return errors.New("usage: sshctl " + groupHelp)
}

// withGroup finds the Group arg (see findGroup) and runs do with its Space,
// as Groups refer to it, and its name.
func (a *app) withGroup(space spaceOption, arg string, do func(ref, name string) error) error {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	ref, name, err := findGroup(snap, space, arg)
	if err != nil {
		return err
	}
	return do(ref, name)
}

// findGroup returns the Space, as Groups refer to it, and the name of the
// Group arg, a Group name or, since a name is never only digits, a Group
// number. With --space it is that Space's Group. Without, a name is looked
// up in every Space and refused, naming them, when several have it; a
// number counts within a Space, so it is refused while there are several.
func findGroup(snap catalog.Snapshot, space spaceOption, arg string) (ref, name string, err error) {
	spaces, err := space.spaces(snap)
	if err != nil {
		return "", "", err
	}
	if n, atoiErr := strconv.Atoi(arg); atoiErr == nil && strings.Trim(arg, "0123456789") == "" {
		if len(spaces) > 1 {
			return "", "", fmt.Errorf("group number %d counts within a space and there are several; pick one with --space", n)
		}
		names := snap.GroupNames(spaces[0].ref)
		if n < 1 || n > len(names) {
			return "", "", fmt.Errorf("no group number %d (there are %d groups)", n, len(names))
		}
		return spaces[0].ref, names[n-1], nil
	}
	var found []spaceIn
	var in []string
	for _, s := range spaces {
		if snap.HasGroup(s.ref, arg) {
			found = append(found, s)
			in = append(in, s.name)
		}
	}
	switch {
	case len(found) == 1:
		return found[0].ref, arg, nil
	case len(found) > 1:
		return "", "", fmt.Errorf("group %q is in spaces %s; pick one with --space", arg, strings.Join(in, ", "))
	case space.set:
		return "", "", fmt.Errorf("no group %q in space %s", arg, space.name)
	}
	return "", "", fmt.Errorf("no group %q", arg)
}

// groupList prints each Group's number, name, member count and dotfiles
// setting, Space by Space. With --space it lists that Space's Groups; with
// several Spaces and no --space, each line starts with the Space's name.
func (a *app) groupList(space spaceOption) error {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	spaces, err := space.spaces(snap)
	if err != nil {
		return err
	}
	dotfiles := map[groupKey]string{}
	for _, g := range snap.Groups {
		dotfiles[groupKey{g.Space, g.Name}] = g.Dotfiles
	}
	members := memberCounts(snap)
	for _, s := range spaces {
		prefix := ""
		if len(spaces) > 1 {
			prefix = fmt.Sprintf("%-20s ", s.name)
		}
		for i, name := range snap.GroupNames(s.ref) {
			k := groupKey{s.ref, name}
			fmt.Fprintf(a.out, "%s%-2d %-20s %-2d %s\n", prefix, i+1, name, members[k], describeDotfiles(dotfiles[k]))
		}
	}
	return nil
}

// groupDelete deletes the Group arg (see findGroup) after asking, unless yes is set. Its
// Connections become ungrouped; none is deleted.
func (a *app) groupDelete(space spaceOption, arg string, yes bool) error {
	cat := a.catalog()
	snap, err := cat.Snapshot()
	if err != nil {
		return err
	}
	ref, name, err := findGroup(snap, space, arg)
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
