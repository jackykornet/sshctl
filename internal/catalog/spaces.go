package catalog

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sshctl/sshctl/internal/config"
)

var (
	// ErrUnknownSpace is returned when no Space has the name given.
	ErrUnknownSpace = errors.New("no such space")
	// ErrSpaceExists is returned when a Space would take a name another Space has.
	ErrSpaceExists = errors.New("a space with that name already exists")
	// ErrSpaceNumber is returned for a Space number outside 1..MaxSpaces.
	ErrSpaceNumber = fmt.Errorf("Space numbers run from 1 to %d", MaxSpaces)
	// ErrSpaceNumberTaken is returned by AddSpaceAt for a number another
	// Space has.
	ErrSpaceNumberTaken = errors.New("another Space has that number")
	// ErrSpacesFull is returned by AddSpace when every Space number is taken.
	ErrSpacesFull = fmt.Errorf("all %d Space numbers are taken", MaxSpaces)
	// ErrSpaceNotEmpty is returned by DeleteSpace while the Space still
	// holds Groups or Connections.
	ErrSpaceNotEmpty = errors.New("the space is not empty")
	// ErrDefaultSpace is returned by DeleteSpace for the default Space.
	ErrDefaultSpace = errors.New("the default space cannot be deleted")
)

// Every Space method takes a Space by name; "" also names the default Space.

// AddSpace creates an empty Space named name with the lowest free Space
// number. It refuses a name config.CheckSpaceName refuses, one another
// Space has, and a new Space once every number is taken (ErrSpacesFull).
func (c *Catalog) AddSpace(name string) error {
	return c.update(func(cfg *config.Config) error {
		n := cfg.FreeSpaceNumber()
		if n == 0 {
			return ErrSpacesFull
		}
		return addSpace(cfg, name, n)
	})
}

// AddSpaceAt creates an empty Space named name with Space number n, as
// AddSpace does. It refuses a number outside 1..MaxSpaces (ErrSpaceNumber)
// and one another Space has (ErrSpaceNumberTaken).
func (c *Catalog) AddSpaceAt(name string, n int) error {
	return c.update(func(cfg *config.Config) error {
		if err := checkSpaceNumber(n); err != nil {
			return err
		}
		if i := cfg.SpaceNumbered(n); i >= 0 {
			return numberTaken(cfg, i)
		}
		return addSpace(cfg, name, n)
	})
}

// addSpace adds the Space name with the free number n.
func addSpace(cfg *config.Config, name string, n int) error {
	if err := freeSpaceName(cfg, name); err != nil {
		return err
	}
	cfg.Spaces = append(cfg.Spaces, Space{Name: name, Number: n})
	cfg.SortSpaces()
	return nil
}

// numberTaken is ErrSpaceNumberTaken for the number of cfg.Spaces[i].
func numberTaken(cfg *config.Config, i int) error {
	s := cfg.Spaces[i]
	return fmt.Errorf("%d is %q: %w", s.Number, s.Name, ErrSpaceNumberTaken)
}

// checkSpaceNumber refuses a Space number outside 1..MaxSpaces.
func checkSpaceNumber(n int) error {
	if n < 1 || n > MaxSpaces {
		return fmt.Errorf("%d: %w", n, ErrSpaceNumber)
	}
	return nil
}

// RenameSpace renames the Space old to name. Its Groups and Connections
// move with it, and it keeps its number, collapsed Groups and whether it
// is active. The default Space can be renamed and stays the default.
func (c *Catalog) RenameSpace(old, name string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, old)
		if err != nil {
			return err
		}
		if cfg.Spaces[cfg.SpaceIndex(ref)].Name == name {
			return nil
		}
		if err := freeSpaceName(cfg, name); err != nil {
			return err
		}
		cfg.RenameSpace(ref, name)
		return nil
	})
}

// DeleteSpace deletes the Space name. It refuses the default Space
// (ErrDefaultSpace) and, with ErrSpaceNotEmpty and what is left in it, a
// Space that still has Groups or Connections.
func (c *Catalog) DeleteSpace(name string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, name)
		if err != nil {
			return err
		}
		if ref == "" {
			return ErrDefaultSpace
		}
		groups, aliases := cfg.SpaceContents(ref)
		var left []string
		if len(groups) > 0 {
			left = append(left, "groups "+strings.Join(groups, ", "))
		}
		if len(aliases) > 0 {
			left = append(left, "connections "+strings.Join(aliases, ", "))
		}
		if len(left) > 0 {
			return fmt.Errorf("%q still has %s: %w", name, strings.Join(left, " and "), ErrSpaceNotEmpty)
		}
		cfg.DeleteSpace(ref)
		return nil
	})
}

// MoveSpace gives the Space name Space number n. A Space that already has
// n swaps numbers with it; no other Space's number changes. It refuses an
// unknown Space, a number outside 1..MaxSpaces (ErrSpaceNumber), and a
// taken number for a Space that has none to swap (ErrSpaceNumberTaken).
func (c *Catalog) MoveSpace(name string, n int) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, name)
		if err != nil {
			return err
		}
		if err := checkSpaceNumber(n); err != nil {
			return err
		}
		i := cfg.SpaceIndex(ref)
		if j := cfg.SpaceNumbered(n); j >= 0 {
			if cfg.Spaces[i].Number == 0 {
				return numberTaken(cfg, j)
			}
			cfg.Spaces[j].Number = cfg.Spaces[i].Number
		}
		cfg.Spaces[i].Number = n
		cfg.SortSpaces()
		return nil
	})
}

// SetSpace puts alias in the Space space, keeping its description. Its
// Group belongs to its old Space, so it becomes ungrouped. Only sshctl
// metadata changes; no Host block moves.
func (c *Catalog) SetSpace(alias, space string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		m := cfg.Connections[alias]
		if m.Space == ref {
			return nil
		}
		cfg.SetMetadata(alias, ref, m.Description, "")
		return nil
	})
}

// SetSpaceDotfiles saves the dotfiles setting of the Space space: a repo
// URL, config.DotfilesNone, or "" to unset it, so the global setting
// applies to its Connections whose Group sets none.
func (c *Catalog) SetSpaceDotfiles(space, repo string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		cfg.Spaces[cfg.SpaceIndex(ref)].Dotfiles = repo
		return nil
	})
}

// SetActiveSpace saves the Space the Workspace shows on this machine.
func (c *Catalog) SetActiveSpace(space string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		cfg.ActiveSpace = ref
		return nil
	})
}

// spaceRef returns how Groups and Connections refer to the Space named
// name ("" for the default Space), or ErrUnknownSpace.
func spaceRef(cfg *config.Config, name string) (string, error) {
	ref, ok := cfg.SpaceRef(name)
	if !ok {
		return "", fmt.Errorf("%q: %w", name, ErrUnknownSpace)
	}
	return ref, nil
}

// freeSpaceName refuses name for a new or renamed Space.
func freeSpaceName(cfg *config.Config, name string) error {
	if err := config.CheckSpaceName(name); err != nil {
		return err
	}
	if slices.ContainsFunc(cfg.Spaces, func(s Space) bool { return s.Name == name }) {
		return fmt.Errorf("%q: %w", name, ErrSpaceExists)
	}
	return nil
}
