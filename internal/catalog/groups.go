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

// Every Group method takes the Space by name, "" for the default Space, and
// refuses an unknown one with ErrUnknownSpace.

// AddGroup creates an empty Group named name after space's other Groups. It
// refuses a name config.CheckGroupName refuses and one another Group of
// space has.
func (c *Catalog) AddGroup(space, name string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		if err := freeGroupName(cfg, ref, name); err != nil {
			return err
		}
		cfg.Groups = append(cfg.Groups, Group{Name: name, Space: ref})
		return nil
	})
}

// RenameGroup renames space's Group old to name. Its members move with it
// and it keeps its position, settings and collapsed state. It refuses an
// unknown Group, a name another Group of space has and one
// config.CheckGroupName refuses.
func (c *Catalog) RenameGroup(space, old, name string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		if err := knownGroup(cfg, ref, old); err != nil {
			return err
		}
		if old == name {
			return nil
		}
		if err := freeGroupName(cfg, ref, name); err != nil {
			return err
		}
		cfg.RenameGroup(ref, old, name)
		return nil
	})
}

// DeleteGroup deletes space's Group name. Its Connections become ungrouped
// (one with no other metadata left keeps none); no Connection or Host block
// is deleted.
func (c *Catalog) DeleteGroup(space, name string) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		if err := knownGroup(cfg, ref, name); err != nil {
			return err
		}
		cfg.DeleteGroup(ref, name)
		return nil
	})
}

// MoveGroup puts space's Group name at position among space's Groups,
// counting from 1 (its new Group number); the Groups between shift by one,
// and other Spaces' Groups stay where they are. It refuses an unknown Group
// and a position outside 1..the number of space's Groups.
func (c *Catalog) MoveGroup(space, name string, position int) error {
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		if err := knownGroup(cfg, ref, name); err != nil {
			return err
		}
		// slots are the positions in cfg.Groups that ref's Groups take.
		var slots []int
		var groups []Group
		for i, g := range cfg.Groups {
			if g.Space == ref {
				slots = append(slots, i)
				groups = append(groups, g)
			}
		}
		if position < 1 || position > len(groups) {
			return fmt.Errorf("%d: %w (there are %d groups)", position, ErrGroupPosition, len(groups))
		}
		i := slices.IndexFunc(groups, func(g Group) bool { return g.Name == name })
		g := groups[i]
		groups = slices.Insert(slices.Delete(groups, i, i+1), position-1, g)
		for j, slot := range slots {
			cfg.Groups[slot] = groups[j]
		}
		return nil
	})
}

// knownGroup refuses a name no Group of the Space space has.
func knownGroup(cfg *config.Config, space, name string) error {
	if cfg.GroupIndex(space, name) < 0 {
		return fmt.Errorf("%q: %w", name, ErrUnknownGroup)
	}
	return nil
}

// freeGroupName refuses name for a new or renamed Group of the Space space.
func freeGroupName(cfg *config.Config, space, name string) error {
	if err := config.CheckGroupName(name); err != nil {
		return err
	}
	if cfg.GroupIndex(space, name) >= 0 {
		return fmt.Errorf("%q: %w", name, ErrGroupExists)
	}
	return nil
}
