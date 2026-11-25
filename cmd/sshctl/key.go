package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// keyList lists the public keys and certificates in ~/.ssh and the
// registered key paths, marking the Selected identities.
func (a *app) keyList(input) error {
	keys, err := a.catalog().Keys()
	if err != nil {
		return err
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	for _, k := range keys {
		var tags []string
		if isKey(k, snap.Identity) {
			tags = append(tags, "Selected identity")
		}
		var spaces []string
		for ref, key := range snap.SpaceIdentities {
			if isKey(k, key) {
				spaces = append(spaces, "Selected identity of space "+snap.SpaceName(ref))
			}
		}
		sort.Strings(spaces)
		tags = append(tags, spaces...)
		if len(tags) > 0 {
			k += " (" + strings.Join(tags, ", ") + ")"
		}
		fmt.Fprintln(a.out, k)
	}
	return nil
}

// keyAdd registers a private key path.
func (a *app) keyAdd(in input) error {
	args := in.args
	if len(args) != 1 {
		return errUsage
	}
	abs, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	if _, err = os.Stat(abs); err != nil {
		return err
	}
	for _, k := range a.cfg.Keys {
		if k == abs {
			return nil
		}
	}
	a.cfg.Keys = append(a.cfg.Keys, abs)
	return a.save()
}

// keyUse selects the key path args name, or none, as the global Selected
// identity or, with --space, a Space's.
func (a *app) keyUse(in input) error {
	rest := in.args
	space, err := a.spaceOption(in)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errUsage
	}
	k := rest[0]
	if k != "none" {
		abs, err := filepath.Abs(k)
		if err != nil {
			return err
		}
		k = abs
		if _, err = os.Stat(k); err != nil {
			return err
		}
	} else {
		k = ""
	}
	if space.set {
		// none unsets the Space's own, so the global one applies.
		return a.catalog().SetSpaceIdentity(space.name, k)
	}
	return a.catalog().SetIdentity(k)
}

// isKey reports whether the key file k (a private key or its .pub) is the
// private key selected, which is set.
func isKey(k, selected string) bool {
	return selected != "" && (k == selected || strings.TrimSuffix(k, ".pub") == selected)
}
