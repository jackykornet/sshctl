package catalog

import (
	"errors"
	"fmt"
	"slices"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/tunnel"
)

// Tunnel is a saved local forward on a Connection (ADR-0012).
type Tunnel = tunnel.Tunnel

var (
	// ErrUnknownConnection is returned for an alias that is neither in
	// ssh_config nor has sshctl metadata.
	ErrUnknownConnection = errors.New("no such connection")
	// ErrDuplicateTunnel is returned by AddTunnel for a local port or name
	// the Connection already has.
	ErrDuplicateTunnel = tunnel.ErrDuplicate
	// ErrNoTunnel is returned by RemoveTunnel when no Tunnel matches.
	ErrNoTunnel = tunnel.ErrNoTunnel
)

// Tunnels returns alias's Tunnels as the config holds them now. It refuses
// an alias that is no Connection (ErrUnknownConnection).
func (c *Catalog) Tunnels(alias string) ([]Tunnel, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return nil, err
	}
	if err := c.requireConnection(cfg, alias); err != nil {
		return nil, err
	}
	return cfg.Connections[alias].Tunnels, nil
}

// AddTunnel saves t as alias's last Tunnel. It refuses an alias that is no
// Connection (ErrUnknownConnection), and a local port or name alias already
// has (ErrDuplicateTunnel).
func (c *Catalog) AddTunnel(alias string, t Tunnel) error {
	return c.update(func(cfg *config.Config) error {
		if err := c.requireConnection(cfg, alias); err != nil {
			return err
		}
		tunnels, err := tunnel.Add(cfg.Connections[alias].Tunnels, t)
		if err != nil {
			return fmt.Errorf("%s: %w", alias, err)
		}
		cfg.SetTunnels(alias, tunnels)
		return nil
	})
}

// RemoveTunnel deletes alias's Tunnel ref names, by name or, when ref is only
// digits, by local port, and returns it. It returns ErrNoTunnel when there is
// no such Tunnel.
func (c *Catalog) RemoveTunnel(alias, ref string) (Tunnel, error) {
	var removed Tunnel
	err := c.update(func(cfg *config.Config) error {
		tunnels, t, err := tunnel.Remove(cfg.Connections[alias].Tunnels, ref)
		if err != nil {
			return fmt.Errorf("%s: %w", alias, err)
		}
		removed = t
		cfg.SetTunnels(alias, tunnels)
		return nil
	})
	return removed, err
}

// requireConnection returns ErrUnknownConnection unless alias is in
// ssh_config or has sshctl metadata.
func (c *Catalog) requireConnection(cfg *config.Config, alias string) error {
	if _, ok := cfg.Connections[alias]; ok {
		return nil
	}
	aliases, err := c.ssh.Aliases()
	if err != nil {
		return err
	}
	if !slices.Contains(aliases, alias) {
		return fmt.Errorf("%w %q", ErrUnknownConnection, alias)
	}
	return nil
}
