package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/sshctl/sshctl/internal/openssh"
	"github.com/sshctl/sshctl/internal/tunnel"
)

// tunnelRun runs every Tunnel of a Connection in the foreground, as one
// ssh -N, until ssh exits or the user presses Ctrl-C. ssh may prompt in the
// terminal, so it runs without BatchMode. It returns ssh's exit status and
// does not record History: a tunnel is not a Session.
func (a *app) tunnelRun(args []string) error {
	if len(args) != 1 {
		return errUsage
	}
	alias := args[0]
	tunnels, err := a.catalog().Tunnels(alias)
	if err != nil {
		return err
	}
	if len(tunnels) == 0 {
		return fmt.Errorf("%s has no tunnels; add one with: sshctl tunnel add %s %s", alias, alias, tunnel.SpecSyntax)
	}
	key, err := a.identityFor(alias)
	if err != nil {
		return err
	}
	printTunnels(a.out, withAlias(alias, tunnels), false)
	fmt.Fprintln(a.out, "Press Ctrl-C to stop.")
	// Ctrl-C reaches ssh too, which stops; sshctl waits for it so it can
	// return ssh's exit status rather than die first.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)
	return a.runSSH(context.Background(), openssh.TunnelArgs(alias, key, tunnels, false), nil)
}

// tunnelAdd saves a Tunnel, given as a spec, on a Connection.
func (a *app) tunnelAdd(args []string) error {
	if len(args) != 2 {
		return errUsage
	}
	t, err := tunnel.Parse(args[1])
	if err != nil {
		return err
	}
	return a.catalog().AddTunnel(args[0], t)
}

// tunnelRemove deletes a Connection's Tunnel by name or local port.
func (a *app) tunnelRemove(args []string) error {
	if len(args) != 2 {
		return errUsage
	}
	_, err := a.catalog().RemoveTunnel(args[0], args[1])
	return err
}

// tunnelList prints one Connection's Tunnels or, with no alias, every
// Connection's, in the order list shows the Connections.
func (a *app) tunnelList(args []string) error {
	if len(args) > 1 {
		return errUsage
	}
	if len(args) == 1 {
		tunnels, err := a.catalog().Tunnels(args[0])
		if err != nil {
			return err
		}
		printTunnels(a.out, withAlias(args[0], tunnels), false)
		return nil
	}
	snap, err := a.catalog().Snapshot()
	if err != nil {
		return err
	}
	var tunnels []aliasTunnel
	for _, e := range snap.Entries {
		for _, t := range e.Tunnels {
			tunnels = append(tunnels, aliasTunnel{e.Alias, t})
		}
	}
	printTunnels(a.out, tunnels, true)
	return nil
}

// aliasTunnel is a Tunnel and the alias of its Connection.
type aliasTunnel struct {
	alias string
	tunnel.Tunnel
}

// withAlias pairs each of alias's Tunnels with alias.
func withAlias(alias string, tunnels []tunnel.Tunnel) []aliasTunnel {
	out := make([]aliasTunnel, len(tunnels))
	for i, t := range tunnels {
		out[i] = aliasTunnel{alias, t}
	}
	return out
}

// printTunnels prints a line per Tunnel, "label  http://localhost:port →
// host:port", in aligned columns; showAlias starts each line with the
// Tunnel's Connection.
func printTunnels(w io.Writer, tunnels []aliasTunnel, showAlias bool) {
	aliasWidth, labelWidth := 0, 0
	for _, t := range tunnels {
		labelWidth = max(labelWidth, len(t.Label()))
		aliasWidth = max(aliasWidth, len(t.alias))
	}
	for _, t := range tunnels {
		var b strings.Builder
		if showAlias {
			fmt.Fprintf(&b, "%-*s  ", aliasWidth, t.alias)
		}
		fmt.Fprintf(&b, "%-*s  http://localhost:%d → %s:%d", labelWidth, t.Label(), t.LocalPort, t.RemoteHost, t.RemotePort)
		fmt.Fprintln(w, b.String())
	}
}
