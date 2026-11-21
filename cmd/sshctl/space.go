package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

const spaceUsage = "space list | add <name> [<number>] | rename <space> <new> | delete <space> | move <space> <number> | dotfiles <space> [<url|none> | --unset]"

// spaceCommand runs `sshctl space ...`. A Space is given by name or by its
// Space number, which it keeps until it is moved (ADR-0007).
func (a *app) spaceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshctl " + spaceUsage)
	}
	if len(args) > 1 && args[0] != "list" && args[0] != "add" {
		name, err := a.spaceName(args[1])
		if err != nil {
			return err
		}
		args[1] = name
	}
	switch args[0] {
	case "list":
		if len(args) == 1 {
			return a.spaceList()
		}
	case "add":
		switch len(args) {
		case 2:
			return a.catalog().AddSpace(args[1])
		case 3:
			n, err := parseSpaceNumber(args[2])
			if err != nil {
				return err
			}
			return a.catalog().AddSpaceAt(args[1], n)
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
			n, err := parseSpaceNumber(args[2])
			if err != nil {
				return err
			}
			return a.catalog().MoveSpace(args[1], n)
		}
	case "dotfiles":
		// A Space's dotfiles repo is a Provision setting.
		if err := a.checkFeature(config.FeatureProvision); err != nil {
			return err
		}
		return a.spaceDotfiles(args[1:])
	}
	return errors.New("usage: sshctl " + spaceUsage)
}

// parseSpaceNumber reads a Space number given on the command line; the
// catalog refuses one outside 1..config.MaxSpaces.
func parseSpaceNumber(arg string) (int, error) {
	n, ok := number(arg)
	if !ok {
		return 0, fmt.Errorf("%q is not a Space number", arg)
	}
	return n, nil
}

// number reads arg as a number when it is only digits.
func number(arg string) (int, bool) {
	n, err := strconv.Atoi(arg)
	return n, err == nil && strings.Trim(arg, "0123456789") == ""
}

// spaceName returns the name of the Space arg names: a Space name, returned
// as is, or, since a Space name is never only digits, a Space number. It
// refuses a number no Space has.
func (a *app) spaceName(arg string) (string, error) {
	n, ok := number(arg)
	if !ok {
		return arg, nil
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return "", err
	}
	for _, s := range snap.Spaces {
		if s.Number == n {
			return s.Name, nil
		}
	}
	return "", fmt.Errorf("no Space %d", n)
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
		s, ok := snap.Space(args[0])
		if !ok {
			return fmt.Errorf("%q: %w", args[0], catalog.ErrUnknownSpace)
		}
		fmt.Fprintln(a.out, describeDotfiles(s.Dotfiles))
		return nil
	case len(args) == 2 && args[1] == "--unset":
		return a.catalog().SetSpaceDotfiles(args[0], "")
	case len(args) == 2 && args[1] != "" && !strings.HasPrefix(args[1], "-"):
		return a.catalog().SetSpaceDotfiles(args[0], args[1])
	}
	return errors.New("usage: sshctl space dotfiles <space> [<url|none> | --unset]")
}

// spaceList prints each Space's number, name, Group count and Connection
// count, Orphaned ones included, marking the default Space, then the
// dotfiles repo and Selected identity it sets, if any. Spaces come in number
// order, so free numbers leave gaps; a Space without a number (beyond the
// ninth in an older config) comes last, shown as -.
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
	for _, s := range snap.Spaces {
		ref, _ := snap.SpaceRef(s.Name)
		num := "-"
		if s.Number > 0 {
			num = strconv.Itoa(s.Number)
		}
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
		fmt.Fprintf(a.out, "%-2s %-20s %-2d %d%s\n", num, s.Name, groups[ref], conns[ref], tag)
	}
	return nil
}

// spaceOption is a --space option: the Space's name, and whether it was
// given at all.
type spaceOption struct {
	name string
	set  bool
}

// cutSpaceOption takes "--space <space>" out of args, wherever it is, and
// returns the rest. The Space is a name or a Space number (see spaceName).
// It refuses --space twice, without a Space, or with a number no Space has.
func (a *app) cutSpaceOption(args []string) (spaceOption, []string, error) {
	var opt spaceOption
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] != "--space" {
			rest = append(rest, args[i])
			continue
		}
		if opt.set || i+1 == len(args) || args[i+1] == "" {
			return opt, nil, errors.New("--space needs a Space name or number, given once")
		}
		i++
		name, err := a.spaceName(args[i])
		if err != nil {
			return opt, nil, err
		}
		opt = spaceOption{name: name, set: true}
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
