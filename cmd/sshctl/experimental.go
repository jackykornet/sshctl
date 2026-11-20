package main

import (
	"errors"
	"fmt"

	"github.com/sshctl/sshctl/internal/config"
)

const experimentalUsage = "experimental list | enable <feature> | disable <feature>"

// experimentalCommand runs `sshctl experimental ...`: it lists the
// Experimental features and whether each is on, or turns one on or off.
func (a *app) experimentalCommand(args []string) error {
	switch {
	case len(args) == 1 && args[0] == "list":
		snap, err := a.catalog().Snapshot()
		if err != nil {
			return err
		}
		for _, f := range config.Features {
			state := "off"
			if snap.Experimental.On(f) {
				state = "on"
			}
			fmt.Fprintf(a.out, "%-10s %s\n", f, state)
		}
		return nil
	case len(args) == 2 && (args[0] == "enable" || args[0] == "disable"):
		return a.catalog().SetFeature(args[1], args[0] == "enable")
	}
	return errors.New("usage: sshctl " + experimentalUsage)
}

// checkFeature refuses to go on while the Experimental feature is off.
func (a *app) checkFeature(feature string) error { return a.catalog().CheckFeature(feature) }
