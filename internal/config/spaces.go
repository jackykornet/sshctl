package config

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Space is a stored Space record (ADR-0005).
type Space struct {
	Name string `json:"name"`
	// Number is the Space number, 1 to MaxSpaces, which the Space keeps
	// until the user changes it (ADR-0010), or 0 for a Space that has none
	// because every number was taken when Load numbered it; Load leaves it
	// without one until the user gives it a number.
	Number int `json:"number,omitempty"`
	// Default marks the default Space, which holds every Connection and
	// Group not put in another. Groups and Connections refer to it as "",
	// so renaming it touches nothing but this record.
	Default bool `json:"default,omitempty"`
	// Dotfiles is the dotfiles repo URL Provision applies to the Space's
	// Connections whose Group sets none, DotfilesNone to refuse Provision
	// for them, or "" when unset, so the global Provision.Dotfiles applies.
	Dotfiles string `json:"dotfiles,omitempty"`
	// Color is the Space's Indicator color (ADR-0009), one of Colors, or ""
	// for none. A Group's own color wins over it.
	Color string `json:"color,omitempty"`
}

// MaxSpaces is how many Space numbers there are, and so how many Spaces.
const MaxSpaces = 9

// DefaultSpaceName is the default Space's name until the user renames it.
const DefaultSpaceName = "default"

// ErrSpaceName is returned by CheckSpaceName for a name sshctl refuses.
var ErrSpaceName = errors.New("invalid space name")

// CheckSpaceName refuses an empty Space name, one made only of digits,
// which would clash with Space numbers, and one with a "/", which
// separates a Space from a Group in CollapsedKey.
func CheckSpaceName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: a space needs a name", ErrSpaceName)
	case allDigits(name):
		return fmt.Errorf("%w: %q is only digits, which stand for space numbers", ErrSpaceName, name)
	case strings.Contains(name, "/"):
		return fmt.Errorf("%w: %q contains a /", ErrSpaceName, name)
	}
	return nil
}

// SpaceIndex returns the position in Spaces of space as Groups and
// Connections refer to it ("" for the default Space), or -1.
func (c *Config) SpaceIndex(space string) int { return spaceIndex(c.Spaces, space) }

func spaceIndex(spaces []Space, space string) int {
	return slices.IndexFunc(spaces, func(s Space) bool {
		if space == "" {
			return s.Default
		}
		return !s.Default && s.Name == space
	})
}

// SpaceRef returns how Groups and Connections refer to the Space named
// name: "" for the default Space, whether it is given by its name or as "",
// and name itself for any other. It reports false when no Space has that
// name.
func (c *Config) SpaceRef(name string) (string, bool) {
	for _, s := range c.Spaces {
		if s.Name == name || (name == "" && s.Default) {
			if s.Default {
				return "", true
			}
			return name, true
		}
	}
	return "", false
}

// addMissingSpaces gives c a default Space when it has none, and a record
// to every Space a Group or Connection names but Spaces lacks,
// alphabetically after the existing ones. It returns the names of the
// records it added, for numberSpaces to number.
func (c *Config) addMissingSpaces() []string {
	var added []string
	if i := slices.IndexFunc(c.Spaces, func(s Space) bool { return s.Default }); i < 0 {
		name := DefaultSpaceName
		for n := 2; slices.ContainsFunc(c.Spaces, func(s Space) bool { return s.Name == name }); n++ {
			name = fmt.Sprintf("%s-%d", DefaultSpaceName, n)
		}
		c.Spaces = slices.Insert(c.Spaces, 0, Space{Name: name, Default: true})
		added = append(added, name)
	}
	var missing []string
	note := func(space string) {
		if space != "" && c.SpaceIndex(space) < 0 && !slices.Contains(missing, space) {
			missing = append(missing, space)
		}
	}
	for _, g := range c.Groups {
		note(g.Space)
	}
	for _, conn := range c.Connections {
		note(conn.Space)
	}
	sort.Strings(missing)
	for _, name := range missing {
		c.Spaces = append(c.Spaces, Space{Name: name})
	}
	return append(added, missing...)
}

// numberSpaces gives the lowest free Space number, in Spaces order, to
// every Space whose number is out of range or one an earlier Space has, to
// every Space named in added, and, when all is set (migrating from before
// Version 5), to every Space without a number. Any other Space without a
// number keeps none (ADR-0010). Once all MaxSpaces numbers are taken, the
// rest keep 0. It then sorts Spaces by number, unnumbered last, and
// reports whether it changed c.
func (c *Config) numberSpaces(all bool, added []string) bool {
	before := slices.Clone(c.Spaces)
	var unnumbered []int
	for i, s := range c.Spaces {
		bad := s.Number < 0 || s.Number > MaxSpaces || (s.Number > 0 && c.SpaceNumbered(s.Number) < i)
		wanted := s.Number == 0 && (all || slices.Contains(added, s.Name))
		if bad || wanted {
			c.Spaces[i].Number = 0
			unnumbered = append(unnumbered, i)
		}
	}
	for _, i := range unnumbered {
		c.Spaces[i].Number = c.FreeSpaceNumber()
	}
	c.SortSpaces()
	return !slices.Equal(before, c.Spaces)
}

// SpaceNumbered returns the position in Spaces of the Space with number
// n, or -1 when n is free.
func (c *Config) SpaceNumbered(n int) int {
	return slices.IndexFunc(c.Spaces, func(s Space) bool { return s.Number == n })
}

// FreeSpaceNumber returns the lowest Space number no Space has, or 0 when
// all MaxSpaces are taken.
func (c *Config) FreeSpaceNumber() int {
	for n := 1; n <= MaxSpaces; n++ {
		if c.SpaceNumbered(n) < 0 {
			return n
		}
	}
	return 0
}

// SortSpaces puts Spaces in Space number order, any without a number last
// in the order they had.
func (c *Config) SortSpaces() {
	slices.SortStableFunc(c.Spaces, func(a, b Space) int {
		return spaceOrder(a) - spaceOrder(b)
	})
}

// spaceOrder sorts a Space without a number after every numbered one.
func spaceOrder(s Space) int {
	if s.Number == 0 {
		return MaxSpaces + 1
	}
	return s.Number
}

// RenameSpace renames the Space space ("" for the default Space) to name,
// in its record, its Groups and Connections, the collapsed list and the
// active Space. It does not check name; see CheckSpaceName.
func (c *Config) RenameSpace(space, name string) {
	i := c.SpaceIndex(space)
	if i < 0 {
		return
	}
	c.Spaces[i].Name = name
	if space == "" {
		return
	}
	for j, g := range c.Groups {
		if g.Space == space {
			c.Groups[j].Space = name
		}
	}
	for alias, conn := range c.Connections {
		if conn.Space == space {
			conn.Space = name
			c.Connections[alias] = conn
		}
	}
	for j, key := range c.CollapsedGroups {
		if group, ok := strings.CutPrefix(key, space+"/"); ok {
			c.CollapsedGroups[j] = CollapsedKey(name, group)
		}
	}
	if c.ActiveSpace == space {
		c.ActiveSpace = name
	}
	if key, ok := c.SpaceIdentities[space]; ok {
		delete(c.SpaceIdentities, space)
		c.SpaceIdentities[name] = key
	}
}

// DeleteSpace removes the record of the Space space, which must not be the
// default Space and should hold nothing (see SpaceContents), and makes the
// default Space active if space was.
func (c *Config) DeleteSpace(space string) {
	if space == "" {
		return
	}
	if i := c.SpaceIndex(space); i >= 0 {
		c.Spaces = slices.Delete(c.Spaces, i, i+1)
	}
	c.CollapsedGroups = slices.DeleteFunc(c.CollapsedGroups, func(key string) bool { return strings.HasPrefix(key, space+"/") })
	if c.ActiveSpace == space {
		c.ActiveSpace = ""
	}
	delete(c.SpaceIdentities, space)
}

// IdentityFor returns the Selected identity for the Connections of space
// ("" for the default Space): the Space's own, or else the global one.
func (c *Config) IdentityFor(space string) string {
	if key, ok := c.SpaceIdentities[space]; ok {
		return key
	}
	return c.SelectedIdentity
}

// SpaceContents lists the Groups of space and the Connections whose
// metadata puts them in it, each sorted by name.
func (c *Config) SpaceContents(space string) (groups, aliases []string) {
	for _, g := range c.Groups {
		if g.Space == space {
			groups = append(groups, g.Name)
		}
	}
	for alias, conn := range c.Connections {
		if conn.Space == space {
			aliases = append(aliases, alias)
		}
	}
	sort.Strings(groups)
	sort.Strings(aliases)
	return groups, aliases
}
