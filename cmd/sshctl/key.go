package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (a *app) keys(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sshctl key list|use|add")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	keyDir := filepath.Join(home, ".ssh")
	switch args[0] {
	case "list":
		set := map[string]bool{}
		entries, _ := os.ReadDir(keyDir)
		for _, e := range entries {
			n := e.Name()
			if strings.HasSuffix(n, ".pub") || strings.HasSuffix(n, "-cert.pub") {
				set[filepath.Join(keyDir, n)] = true
			}
		}
		for _, k := range a.cfg.Keys {
			set[k] = true
		}
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
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
	case "add":
		if len(args) != 2 {
			return errors.New("usage: sshctl key add <private-key-path>")
		}
		abs, err := filepath.Abs(args[1])
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
	case "use":
		space, rest, err := a.cutSpaceOption(args[1:])
		if err != nil {
			return err
		}
		if len(rest) != 1 {
			return errors.New("usage: sshctl key use [--space <space>] <key-path|none>")
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
	return errors.New("usage: sshctl key list|use|add")
}

// isKey reports whether the key file k (a private key or its .pub) is the
// private key selected, which is set.
func isKey(k, selected string) bool {
	return selected != "" && (k == selected || strings.TrimSuffix(k, ".pub") == selected)
}
