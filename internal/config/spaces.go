package config

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Space is a stored Space record (ADR-0005). Its position in Config.Spaces
// is its Space number, counting from 1.
type Space struct {
	Name string `json:"name"`
	// Default marks the default Space, which holds every Connection and
	// Group not put in another. Groups and Connections refer to it as "",
	// so renaming it touches nothing but this record.
	Default bool `json:"default,omitempty"`
	// Dotfiles is the dotfiles repo URL Provision applies to the Space's
	// Connections whose Group sets none, DotfilesNone to refuse Provision
	// for them, or "" when unset, so the global Provision.Dotfiles applies.
	Dotfiles string `json:"dotfiles,omitempty"`
}

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
func (c *Config) SpaceIndex(space string) int {
	return slices.IndexFunc(c.Spaces, func(s Space) bool {
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
// alphabetically after the existing ones. It reports whether it changed c.
func (c *Config) addMissingSpaces() bool {
	changed := false
	if i := slices.IndexFunc(c.Spaces, func(s Space) bool { return s.Default }); i < 0 {
		name := DefaultSpaceName
		for n := 2; slices.ContainsFunc(c.Spaces, func(s Space) bool { return s.Name == name }); n++ {
			name = fmt.Sprintf("%s-%d", DefaultSpaceName, n)
		}
		c.Spaces = slices.Insert(c.Spaces, 0, Space{Name: name, Default: true})
		changed = true
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
	return changed || len(missing) > 0
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
