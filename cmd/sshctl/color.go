package main

import (
	"strings"

	"github.com/sshctl/sshctl/internal/config"
)

// The color commands set the Indicator color of a Group or a Space
// (ADR-0009), the same field the Workspace's C sets (ADR-0008).

// colorHelp is the help of the color command of a kind ("Group").
func colorHelp(kind string) string {
	return "Set or clear (none) a " + kind + "'s Indicator color:\n" + strings.Join(config.Colors, ", ")
}

// groupColor sets the Indicator color of the Group args[0] to args[1].
func (a *app) groupColor(space spaceOption, in input) error {
	if len(in.args) != 2 {
		return errUsage
	}
	color, err := config.ParseColor(in.args[1])
	if err != nil {
		return err
	}
	return a.withGroup(space, in.args[0], func(ref, name string) error {
		return a.catalog().SetGroupColor(ref, name, color)
	})
}

// spaceColor sets the Indicator color of the Space args[0] to args[1].
func (a *app) spaceColor(in input) error {
	if len(in.args) != 2 {
		return errUsage
	}
	color, err := config.ParseColor(in.args[1])
	if err != nil {
		return err
	}
	space, err := a.spaceName(in.args[0])
	if err != nil {
		return err
	}
	return a.catalog().SetSpaceColor(space, color)
}

// colorTag is how a list shows an Indicator color: "  color red", or ""
// for none.
func colorTag(color string) string {
	if color == "" {
		return ""
	}
	return "  color " + color
}
