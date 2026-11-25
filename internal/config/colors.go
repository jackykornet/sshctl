package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Colors lists the Indicator colors a Group or Space can carry (ADR-0009),
// basic ANSI names only, in the order the CLI and the Workspace offer them.
// "" is no color.
var Colors = []string{"red", "yellow", "green", "cyan", "blue", "magenta"}

// ColorNone is how the CLI and the Workspace name no Indicator color.
const ColorNone = "none"

// ErrUnknownColor is returned by CheckColor for a name Colors does not list.
var ErrUnknownColor = errors.New("unknown color")

// CheckColor refuses an Indicator color Colors does not list; "" (no
// color) is accepted.
func CheckColor(name string) error {
	if name == "" || slices.Contains(Colors, name) {
		return nil
	}
	return fmt.Errorf("%w %q; the colors are %s", ErrUnknownColor, name, colorList())
}

// ParseColor reads an Indicator color as the user names it: ColorNone for
// none (""), or one of Colors. It refuses anything else, "" included, with
// ErrUnknownColor.
func ParseColor(name string) (string, error) {
	switch {
	case name == ColorNone:
		return "", nil
	case name != "" && slices.Contains(Colors, name):
		return name, nil
	}
	return "", fmt.Errorf("%w %q; the colors are %s", ErrUnknownColor, name, colorList())
}

// colorList names every choice: none, then Colors.
func colorList() string {
	return strings.Join(append([]string{ColorNone}, Colors...), ", ")
}

// noteUnknownColors adds a notice for every Group or Space whose color
// Colors does not list, as from a newer sshctl or a hand edit. The color is
// kept, so saving does not drop it, and draws as none.
func (c *Config) noteUnknownColors() {
	note := func(kind, name, color string) {
		if CheckColor(color) != nil {
			c.Notices = append(c.Notices, fmt.Sprintf("%s %q has unknown color %q, shown as none; the colors are %s", kind, name, color, strings.Join(Colors, ", ")))
		}
	}
	for _, s := range c.Spaces {
		note("space", s.Name, s.Color)
	}
	for _, g := range c.Groups {
		note("group", g.Name, g.Color)
	}
}

// ColorFor returns the Indicator color of space's Group group ("" for an
// ungrouped Connection), resolved Group → Space → none: the Group's own
// color, else its Space's, else "". A color CheckColor refuses counts as
// none.
func (c *Config) ColorFor(space, group string) string {
	if i := c.GroupIndex(space, group); group != "" && i >= 0 {
		if color := c.Groups[i].Color; color != "" && CheckColor(color) == nil {
			return color
		}
	}
	if i := c.SpaceIndex(space); i >= 0 {
		if color := c.Spaces[i].Color; CheckColor(color) == nil {
			return color
		}
	}
	return ""
}
