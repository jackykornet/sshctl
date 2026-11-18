package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/openssh"
)

const spaceUsage = "space list | add <name> | rename <old> <new> | delete <name> | move <name> <position> | dotfiles <name> [<url|none> | --unset]"

// spaceCommand runs `sshctl space ...`. Spaces are given by name, which
// keeps scripts working when Spaces are reordered.
func (a *app) spaceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshctl " + spaceUsage)
	}
	switch args[0] {
	case "list":
		if len(args) == 1 {
			return a.spaceList()
		}
	case "add":
		if len(args) == 2 {
			return a.catalog().AddSpace(args[1])
		}
	case "rename":
		if len(args) == 3 {
			return a.catalog().RenameSpace(args[1], args[2])
		}
	case "delete":
		// Only an empty Space can be deleted, so nothing is lost and
		// there is nothing to confirm.
		if len(args) == 2 {
			return a.catalog().DeleteSpace(args[1])
		}
	case "move":
		if len(args) == 3 {
			position, err := strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("position %q is not a number", args[2])
			}
			return a.catalog().MoveSpace(args[1], position)
		}
	case "dotfiles":
		return a.spaceDotfiles(args[1:])
	}
	return errors.New("usage: sshctl " + spaceUsage)
}

// spaceDotfiles shows the dotfiles setting of the Space args[0], or sets it
// to args[1] (a repo URL or none) or, with --unset, unsets it so the global
// one applies.
func (a *app) spaceDotfiles(args []string) error {
	switch {
	case len(args) == 1:
		snap, err := a.catalog().Snapshot()
		if err != nil {
			return err
		}
		ref, ok := snap.SpaceRef(args[0])
		if !ok {
			return fmt.Errorf("%q: %w", args[0], catalog.ErrUnknownSpace)
		}
		for _, s := range snap.Spaces {
			if r, _ := snap.SpaceRef(s.Name); r == ref {
				fmt.Fprintln(a.out, describeDotfiles(s.Dotfiles))
			}
		}
		return nil
	case len(args) == 2 && args[1] == "--unset":
		return a.catalog().SetSpaceDotfiles(args[0], "")
	case len(args) == 2 && args[1] != "" && !strings.HasPrefix(args[1], "-"):
		return a.catalog().SetSpaceDotfiles(args[0], args[1])
	}
	return errors.New("usage: sshctl space dotfiles <name> [<url|none> | --unset]")
}

// spaceList prints each Space's number, name, Group count and Connection
// count, Orphaned ones included, marking the default Space, then the
// dotfiles repo and Selected identity it sets, if any.
func (a *app) spaceList() error {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	groups, conns := map[string]int{}, map[string]int{}
	for _, g := range snap.Groups {
		groups[g.Space]++
	}
	for _, e := range snap.Entries {
		conns[e.Space]++
	}
	for i, s := range snap.Spaces {
		ref, _ := snap.SpaceRef(s.Name)
		tag := ""
		if s.Default {
			tag = "  (default)"
		}
		if s.Dotfiles != "" {
			tag += "  dotfiles " + describeDotfiles(s.Dotfiles)
		}
		if key, ok := snap.SpaceIdentities[ref]; ok {
			tag += "  key " + openssh.HomeRelative(key, a.home())
		}
		fmt.Fprintf(a.out, "%-2d %-20s %-2d %d%s\n", i+1, s.Name, groups[ref], conns[ref], tag)
	}
	return nil
}

// spaceOption is a --space option: the Space's name, and whether it was
// given at all.
type spaceOption struct {
	name string
	set  bool
}

// cutSpaceOption takes "--space <name>" out of args, wherever it is, and
// returns the rest. It refuses --space twice or without a name.
func cutSpaceOption(args []string) (spaceOption, []string, error) {
	var opt spaceOption
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] != "--space" {
			rest = append(rest, args[i])
			continue
		}
		if opt.set || i+1 == len(args) || args[i+1] == "" {
			return opt, nil, errors.New("--space needs a space name, given once")
		}
		i++
		opt = spaceOption{name: args[i], set: true}
	}
	return opt, rest, nil
}

// spaceIn is a Space as a command sees it: how Groups and Connections
// refer to it ("" for the default Space) and its name.
type spaceIn struct{ ref, name string }

// spaces returns the Space opt names or, without --space, every Space, in
// order. It refuses an unknown Space, naming the Spaces there are.
func (opt spaceOption) spaces(snap catalog.Snapshot) ([]spaceIn, error) {
	var all []spaceIn
	var names []string
	for _, s := range snap.Spaces {
		ref, _ := snap.SpaceRef(s.Name)
		all = append(all, spaceIn{ref, s.Name})
		names = append(names, s.Name)
	}
	if !opt.set {
		return all, nil
	}
	for _, s := range all {
		if s.name == opt.name {
			return []spaceIn{s}, nil
		}
	}
	return nil, fmt.Errorf("no space %q; the spaces are %s", opt.name, strings.Join(names, ", "))
}
