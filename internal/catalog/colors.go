package catalog

import "github.com/sshctl/sshctl/internal/config"

// Color returns the Indicator color (ADR-0009) of the Space ref's Group
// group ("" for the default Space; group "" for an ungrouped Connection),
// resolved Group → Space → none: the Group's own color, else its Space's,
// else "". A Connection has no color of its own; it takes the one of its
// Group and Space.
func (s Snapshot) Color(ref, group string) string {
	return config.ColorFor(s.Spaces, s.Groups, ref, group)
}

// SetGroupColor saves the Indicator color of the Space space's Group group:
// one of config.Colors, or "" for none, so its Space's applies. It refuses
// any other color with config.ErrUnknownColor, and a Group with no record
// with ErrUnknownGroup.
func (c *Catalog) SetGroupColor(space, group, color string) error {
	if err := config.CheckColor(color); err != nil {
		return err
	}
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		if err := knownGroup(cfg, ref, group); err != nil {
			return err
		}
		cfg.Groups[cfg.GroupIndex(ref, group)].Color = color
		return nil
	})
}

// SetSpaceColor saves the Indicator color of the Space space: one of
// config.Colors, or "" for none. It refuses any other color with
// config.ErrUnknownColor.
func (c *Catalog) SetSpaceColor(space, color string) error {
	if err := config.CheckColor(color); err != nil {
		return err
	}
	return c.update(func(cfg *config.Config) error {
		ref, err := spaceRef(cfg, space)
		if err != nil {
			return err
		}
		cfg.Spaces[cfg.SpaceIndex(ref)].Color = color
		return nil
	})
}
