// Package catalog is the Connection catalog: the one place that reads and
// changes Connections across sshctl's config (description, group, History)
// and ssh_config (Managed connections' Host blocks, ADR-0002).
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

// Host is how OpenSSH reaches a Managed connection.
type Host struct {
	HostName     string
	User         string
	Port         string
	IdentityFile string
}

// Spec is a Connection as it is to be saved.
type Spec struct {
	Alias       string
	Description string
	Group       string
	// Managed is true when sshctl writes the Host block, so Host applies.
	// For a Connection defined elsewhere in ssh_config only the description
	// and group are saved.
	Managed bool
	Host    Host
}

// IncludeError is returned by Save when the user's ssh_config does not yet
// read sshctl's hosts file. Saving can be retried after AddInclude adds Line
// to Config.
type IncludeError struct {
	Line   string
	Config string
}

func (e *IncludeError) Error() string { return e.Config + " does not contain " + e.Line }

// Catalog reads and writes Connections in sshctl's config file and
// ssh_config.
type Catalog struct {
	configPath string
	legacy     []string
	ssh        *openssh.Client
	// saveConfig writes the sshctl config; tests make it fail.
	saveConfig func(string, *config.Config) error
}

// New returns the Connection catalog over the sshctl config at configPath
// (migrated from legacy when missing) and ssh.
func New(configPath string, legacy []string, ssh *openssh.Client) *Catalog {
	return &Catalog{configPath: configPath, legacy: legacy, ssh: ssh, saveConfig: config.Save}
}

// Save adds a Connection (old is "") or saves changes to old.
//
// A Host block is written to sshctl's ssh_config file whenever s is Managed
// or the alias is new or renamed; openssh.SaveHost enforces ADR-0002, and
// a Connection whose Host block is the user's cannot be renamed. Then the
// description and group are saved under the new alias, and History follows
// a rename.
//
// Nothing is changed when Save fails: the sshctl config is read before the
// Host block is written, and the Host block is put back when the config
// cannot be written. Save returns an *IncludeError when ssh_config must
// first Include sshctl's file.
func (c *Catalog) Save(old string, s Spec) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	undo := func() error { return nil }
	if s.Managed || old != s.Alias {
		if undo, err = c.saveHost(old, s); err != nil {
			return err
		}
	}
	if old != "" && old != s.Alias {
		rename(cfg, old, s.Alias)
	}
	cfg.SetMetadata(s.Alias, s.Description, s.Group)
	if err := c.saveConfig(c.configPath, cfg); err != nil {
		if uerr := undo(); uerr != nil {
			return fmt.Errorf("%w (and restoring the Host block failed: %v)", err, uerr)
		}
		return err
	}
	return nil
}

// saveHost writes s's Host block in place of old's and returns how to put
// back what was there before.
func (c *Catalog) saveHost(old string, s Spec) (undo func() error, err error) {
	var prev openssh.Host
	if old != "" {
		hosts, err := c.ssh.ManagedHosts()
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			if h.Alias == old {
				prev = h
			}
		}
	}
	h := openssh.Host{Alias: s.Alias, HostName: s.Host.HostName, User: s.Host.User, Port: s.Host.Port, IdentityFile: s.Host.IdentityFile}
	switch err := c.ssh.SaveHost(old, h); {
	case errors.Is(err, openssh.ErrNotIncluded):
		return nil, &IncludeError{Line: openssh.IncludeLine, Config: displayPath(c.ssh.SSHConfig)}
	case errors.Is(err, openssh.ErrUnknownHost):
		return nil, fmt.Errorf("%s is defined outside sshctl and cannot be renamed here", old)
	case err != nil:
		return nil, err
	}
	if old == "" {
		return func() error { return c.ssh.DeleteHost(s.Alias) }, nil
	}
	return func() error { return c.ssh.SaveHost(s.Alias, prev) }, nil
}

// Delete removes alias's Host block when sshctl manages it, and its sshctl
// metadata. openssh.DeleteHost decides from ssh_config whether the block is
// sshctl's; a block anywhere else is left alone.
func (c *Catalog) Delete(alias string) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	if err := c.ssh.DeleteHost(alias); err != nil && !errors.Is(err, openssh.ErrUnknownHost) {
		return err
	}
	delete(cfg.Connections, alias)
	return c.saveConfig(c.configPath, cfg)
}

// rename is the rename rule: old's metadata and History follow it to alias.
func rename(cfg *config.Config, old, alias string) {
	if m, ok := cfg.Connections[old]; ok {
		delete(cfg.Connections, old)
		m.Alias = alias
		cfg.Connections[alias] = m
	}
	for i, h := range cfg.History {
		if h == old {
			cfg.History[i] = alias
		}
	}
}

// AddInclude adds the line an *IncludeError names to the top of the user's
// ssh_config, so OpenSSH reads sshctl's Host blocks.
func (c *Catalog) AddInclude() error { return c.ssh.AddInclude() }

// displayPath shows path with the home directory as "~".
func displayPath(path string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}
