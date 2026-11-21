package main

import (
	"fmt"

	"github.com/sshctl/sshctl/internal/config"
)

// experimentalList lists the Experimental features and whether each is on.
func (a *app) experimentalList(args []string) error {
	if len(args) != 0 {
		return errUsage
	}
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
}

// experimentalSet turns an Experimental feature on (enable) or off (disable).
func (a *app) experimentalSet(args []string) error {
	if len(args) != 2 || (args[0] != "enable" && args[0] != "disable") {
		return errUsage
	}
	return a.catalog().SetFeature(args[1], args[0] == "enable")
}

// checkFeature refuses to go on while the Experimental feature is off.
func (a *app) checkFeature(feature string) error { return a.catalog().CheckFeature(feature) }
