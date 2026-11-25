package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sshctl/sshctl/internal/catalog"
	"github.com/sshctl/sshctl/internal/openssh"
)

// A Space is given by name or by its Space number, which it keeps until it
// is moved (ADR-0010); see spaceName.

func (a *app) spaceAdd(in input) error {
	args := in.args
	switch len(args) {
	case 1:
		return a.catalog().AddSpace(args[0])
	case 2:
		n, err := spaceNumberArg(args[1])
		if err != nil {
			return err
		}
		return a.catalog().AddSpaceAt(args[0], n)
	}
	return errUsage
}

func (a *app) spaceRename(in input) error {
	args := in.args
	if len(args) != 2 {
		return errUsage
	}
	space, err := a.spaceName(args[0])
	if err != nil {
		return err
	}
	return a.catalog().RenameSpace(space, args[1])
}

// spaceDelete deletes an empty Space. Only an empty Space can be deleted,
// so nothing is lost and there is nothing to confirm.
func (a *app) spaceDelete(in input) error {
	args := in.args
	if len(args) != 1 {
		return errUsage
	}
	space, err := a.spaceName(args[0])
	if err != nil {
		return err
	}
	return a.catalog().DeleteSpace(space)
}

func (a *app) spaceMove(in input) error {
	args := in.args
	if len(args) != 2 {
		return errUsage
	}
	space, err := a.spaceName(args[0])
	if err != nil {
		return err
	}
	n, err := spaceNumberArg(args[1])
	if err != nil {
		return err
	}
	return a.catalog().MoveSpace(space, n)
}

// spaceNumberArg reads a Space number given on the command line, which is
// only digits; the catalog refuses one outside 1..catalog.MaxSpaces.
func spaceNumberArg(arg string) (int, error) {
	n, err := strconv.Atoi(arg)
	if err != nil || strings.Trim(arg, "0123456789") != "" {
		return 0, fmt.Errorf("%q is not a Space number", arg)
	}
	return n, nil
}

// spaceName returns the name of the Space arg names, a Space name or
// number (catalog.Snapshot.ResolveSpace). It refuses a name or number no
// Space has, naming the Spaces there are.
func (a *app) spaceName(arg string) (string, error) {
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return "", err
	}
	ref, err := snap.ResolveSpace(arg)
	if err != nil {
		return "", err
	}
	return snap.SpaceName(ref), nil
}

// spaceDotfiles shows the dotfiles setting of the Space args[0], a
// Provision setting, or sets it to args[1] (a repo URL or none) or, with
// --unset, unsets it so the global one applies.
func (a *app) spaceDotfiles(in input) error {
	args := in.args
	if len(args) == 0 {
		return errUsage
	}
	space, err := a.spaceName(args[0])
	if err != nil {
		return err
	}
	unset := in.flag("unset")
	switch args = args[1:]; {
	case len(args) == 0 && !unset:
		snap, err := a.catalog().Snapshot()
		if err != nil {
			return err
		}
		s, ok := snap.Space(space)
		if !ok {
			return fmt.Errorf("%q: %w", space, catalog.ErrUnknownSpace)
		}
		fmt.Fprintln(a.out, describeDotfiles(s.Dotfiles))
		return nil
	case len(args) == 0:
		return a.catalog().SetSpaceDotfiles(space, "")
	case len(args) == 1 && args[0] != "" && !unset:
		return a.catalog().SetSpaceDotfiles(space, args[0])
	}
	return errUsage
}

// spaceList prints each Space's number, name, Group count and Connection
// count, Orphaned ones included, marking the default Space, then the
// Indicator color, dotfiles repo and Selected identity it sets, if any. Spaces come in number
// order, so free numbers leave gaps; a Space without a number (beyond the
// ninth in an older config) comes last, shown as -.
func (a *app) spaceList(in input) error {
	args := in.args
	if len(args) != 0 {
		return errUsage
	}
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
	if in.flag("json") {
		spaces := []spaceJSON{}
		for _, s := range snap.Spaces {
			ref, _ := snap.SpaceRef(s.Name)
			j := spaceJSON{Name: s.Name, Default: s.Default, Groups: groups[ref], Connections: conns[ref], Color: s.Color, Dotfiles: s.Dotfiles, Identity: snap.SpaceIdentities[ref]}
			if s.Number > 0 {
				j.Number = &s.Number
			}
			spaces = append(spaces, j)
		}
		return a.writeJSON(spaces)
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
		tag += colorTag(s.Color)
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

// spaceOption is a --space option: the Space, as Groups and Connections
// refer to it ("" for the default Space) and by name, and whether it was
// given at all.
type spaceOption struct {
	ref, name string
	set       bool
}

// spaceOption resolves the --space option in gives, if any: a Space name or
// a Space number (see spaceName). It refuses an empty one or one naming no
// Space.
func (a *app) spaceOption(in input) (spaceOption, error) {
	arg, ok := in.value(fieldSpace)
	if !ok {
		return spaceOption{}, nil
	}
	if arg == "" {
		return spaceOption{}, errors.New("--space needs a Space name or number")
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return spaceOption{}, err
	}
	ref, err := snap.ResolveSpace(arg)
	if err != nil {
		return spaceOption{}, err
	}
	return spaceOption{ref: ref, name: snap.SpaceName(ref), set: true}, nil
}

// spaceIn is a Space as a command sees it: how Groups and Connections
// refer to it ("" for the default Space) and its name.
type spaceIn struct{ ref, name string }

// spaces returns the Space opt names or, without --space, every Space, in
// order.
func (opt spaceOption) spaces(snap catalog.Snapshot) []spaceIn {
	if opt.set {
		return []spaceIn{{opt.ref, opt.name}}
	}
	var all []spaceIn
	for _, s := range snap.Spaces {
		ref, _ := snap.SpaceRef(s.Name)
		all = append(all, spaceIn{ref, s.Name})
	}
	return all
}
