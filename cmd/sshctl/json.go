package main

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/sshctl/sshctl/internal/catalog"
)

// The --json output of list, group list and space list, and sshctl info,
// is the stable interface for scripts: the field names below are a
// compatibility promise, documented in the README. Every field is always
// present; "" means none.

// writeJSON prints v as indented JSON and a newline.
func (a *app) writeJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = a.out.Write(append(b, '\n'))
	return err
}

// connectionJSON is a Connection in list --json.
type connectionJSON struct {
	Alias       string `json:"alias"`
	Description string `json:"description"`
	Space       string `json:"space"`
	Group       string `json:"group"`
	Orphaned    bool   `json:"orphaned"`
	// ShadowedByCommand is true for an alias named like a built-in
	// command, which only sshctl -c reaches (ADR-0011).
	ShadowedByCommand bool `json:"shadowedByCommand"`
}

// groupJSON is a Group in group list --json: its Space's name, its number
// within the Space, its name, how many Connections it has (Orphaned ones
// included) and its dotfiles repo ("" unset, "none" refused).
type groupJSON struct {
	Space       string `json:"space"`
	Number      int    `json:"number"`
	Name        string `json:"name"`
	Connections int    `json:"connections"`
	Dotfiles    string `json:"dotfiles"`
}

// spaceJSON is a Space in space list --json: its number (null for a Space
// without one), name, whether it is the default Space, how many Groups and
// Connections (Orphaned ones included) it has, and the dotfiles repo ("" unset,
// "none" refused) and Selected identity ("" none) it sets.
type spaceJSON struct {
	Number      *int   `json:"number"`
	Name        string `json:"name"`
	Default     bool   `json:"default"`
	Groups      int    `json:"groups"`
	Connections int    `json:"connections"`
	Dotfiles    string `json:"dotfiles"`
	Identity    string `json:"identity"`
}

// infoJSON is a Connection in sshctl info: list --json's fields but
// shadowedByCommand, whether sshctl writes its Host block (a Managed
// connection), and the Selected identity ssh would use for it ("" when
// OpenSSH chooses keys).
type infoJSON struct {
	Alias       string `json:"alias"`
	Space       string `json:"space"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Managed     bool   `json:"managed"`
	Orphaned    bool   `json:"orphaned"`
	Identity    string `json:"identity"`
}

// info prints the Connection the alias operand names as JSON, the only
// form for now; --json is accepted so scripts keep working once a text form
// becomes the default. An alias that is no Connection is refused.
func (a *app) info(in input) error {
	if len(in.args) != 1 {
		return errUsage
	}
	cat := a.catalog()
	snap, err := cat.Snapshot()
	if err != nil {
		return err
	}
	e, ok := snap.Entry(in.args[0])
	if !ok {
		return fmt.Errorf("no Connection %q; sshctl list shows them", in.args[0])
	}
	key, err := cat.IdentityFor(e.Alias)
	if err != nil {
		return err
	}
	return a.writeJSON(infoJSON{
		Alias:       e.Alias,
		Space:       snap.SpaceName(e.Space),
		Group:       e.Group,
		Description: e.Description,
		Managed:     e.Managed,
		Orphaned:    e.Orphaned,
		Identity:    key,
	})
}

// connectionsJSON returns entries of snap as list --json shows them.
func connectionsJSON(snap catalog.Snapshot, entries []catalog.Entry) []connectionJSON {
	out := []connectionJSON{}
	for _, e := range entries {
		out = append(out, connectionJSON{
			Alias:             e.Alias,
			Description:       e.Description,
			Space:             snap.SpaceName(e.Space),
			Group:             e.Group,
			Orphaned:          e.Orphaned,
			ShadowedByCommand: slices.Contains(commandNames, e.Alias),
		})
	}
	return out
}
