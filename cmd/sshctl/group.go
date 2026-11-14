package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/sshctl/sshctl/internal/catalog"
)

const groupUsage = "group list | add <name> | rename <old> <new> | delete <name> [--yes] | move <name> <position> | connect <group>"

// groupCommand runs `sshctl group ...`. Groups are named, never numbered,
// so scripts keep working when Groups are reordered.
func (a *app) groupCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshctl " + groupUsage)
	}
	switch args[0] {
	case "list":
		if len(args) == 1 {
			return a.groupList()
		}
	case "add":
		if len(args) == 2 {
			return a.catalog().AddGroup(args[1])
		}
	case "rename":
		if len(args) == 3 {
			return a.catalog().RenameGroup(args[1], args[2])
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
			return a.groupDelete(names[0], yes)
		}
	case "move":
		if len(args) == 3 {
			position, err := strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("position %q is not a number", args[2])
			}
			return a.catalog().MoveGroup(args[1], position)
		}
	case "connect":
		return a.groupConnect(args[1:])
	}
	return errors.New("usage: sshctl " + groupUsage)
}

// groupList prints each Group's number, name, member count and dotfiles
// setting.
func (a *app) groupList() error {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	members := memberCounts(snap)
	for i, g := range snap.Groups {
		fmt.Fprintf(a.out, "%-2d %-20s %-2d %s\n", i+1, g.Name, members[g.Name], describeDotfiles(g.Dotfiles))
	}
	return nil
}

// groupDelete deletes the Group name after asking, unless yes is set. Its
// Connections become ungrouped; none is deleted.
func (a *app) groupDelete(name string, yes bool) error {
	cat := a.catalog()
	snap, err := cat.Snapshot()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(snap.Groups, func(g catalog.Group) bool { return g.Name == name }) {
		return fmt.Errorf("no group %q", name)
	}
	if !yes {
		n := memberCounts(snap)[name]
		noun, verb := "Connections", "become"
		if n == 1 {
			noun, verb = "Connection", "becomes"
		}
		fmt.Fprintf(a.out, "Delete group %s? %d %s %s ungrouped. [y/N] ", name, n, noun, verb)
		if !confirm(a.in) {
			return nil
		}
	}
	return cat.DeleteGroup(name)
}

// memberCounts counts each Group's Connections, Orphaned ones included.
func memberCounts(snap catalog.Snapshot) map[string]int {
	members := map[string]int{}
	for _, e := range snap.Entries {
		members[e.Group]++
	}
	return members
}
