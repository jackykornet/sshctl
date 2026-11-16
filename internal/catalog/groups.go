package catalog

import (
	"errors"
	"fmt"
	"slices"

	"github.com/sshctl/sshctl/internal/config"
)

var (
	// ErrGroupExists is returned when a Group would take a name another Group has.
	ErrGroupExists = errors.New("a group with that name already exists")
	// ErrGroupPosition is returned by MoveGroup for a position with no Group.
	ErrGroupPosition = errors.New("no such group position")
)

// AddGroup creates an empty Group named name after the others. It refuses a
// name config.CheckGroupName refuses and one another Group has.
func (c *Catalog) AddGroup(name string) error {
	return c.update(func(cfg *config.Config) error {
		if err := freeGroupName(cfg, name); err != nil {
			return err
		}
		cfg.Groups = append(cfg.Groups, Group{Name: name})
		return nil
	})
}

// RenameGroup renames the Group old to name. Its members move with it and it
// keeps its position, settings and collapsed state. It refuses an unknown
// Group, a name another Group has and one config.CheckGroupName refuses.
func (c *Catalog) RenameGroup(old, name string) error {
	return c.update(func(cfg *config.Config) error {
		if err := knownGroup(cfg, old); err != nil {
			return err
		}
		if old == name {
			return nil
		}
		if err := freeGroupName(cfg, name); err != nil {
			return err
		}
		cfg.RenameGroup(old, name)
		return nil
	})
}

// DeleteGroup deletes the Group name. Its Connections become ungrouped (one
// with no description left keeps no metadata); no Connection or Host block
// is deleted.
func (c *Catalog) DeleteGroup(name string) error {
	return c.update(func(cfg *config.Config) error {
		if err := knownGroup(cfg, name); err != nil {
			return err
		}
		cfg.DeleteGroup(name)
		return nil
	})
}

// MoveGroup puts the Group name at position, counting from 1 (its new Group
// number); the Groups between shift by one. It refuses an unknown Group and
// a position outside 1..len(Groups).
func (c *Catalog) MoveGroup(name string, position int) error {
	return c.update(func(cfg *config.Config) error {
		if err := knownGroup(cfg, name); err != nil {
			return err
		}
		if position < 1 || position > len(cfg.Groups) {
			return fmt.Errorf("%d: %w (there are %d groups)", position, ErrGroupPosition, len(cfg.Groups))
		}
		i := cfg.GroupIndex(name)
		g := cfg.Groups[i]
		cfg.Groups = slices.Insert(slices.Delete(cfg.Groups, i, i+1), position-1, g)
		return nil
	})
}

// knownGroup refuses a name no Group has.
func knownGroup(cfg *config.Config, name string) error {
	if cfg.GroupIndex(name) < 0 {
		return fmt.Errorf("%q: %w", name, ErrUnknownGroup)
	}
	return nil
}

// freeGroupName refuses name for a new or renamed Group.
func freeGroupName(cfg *config.Config, name string) error {
	if err := config.CheckGroupName(name); err != nil {
		return err
	}
	if cfg.GroupIndex(name) >= 0 {
		return fmt.Errorf("%q: %w", name, ErrGroupExists)
	}
	return nil
}
