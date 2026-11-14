// Package catalog is the Connection catalog: the one place that reads and
// changes Connections across sshctl's config (description, group, History)
// and ssh_config (Managed connections' Host blocks, ADR-0002).
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/sshctl/sshctl/internal/config"
	"github.com/sshctl/sshctl/internal/openssh"
)

// Host is how OpenSSH reaches a Managed connection.
type Host = openssh.Endpoint

// Group is a stored Group record (ADR-0003). A Group's number is its
// position in Snapshot.Groups counting from 1.
type Group = config.Group

// ErrUnknownGroup is returned when a Connection is moved into a Group that
// has no record.
var ErrUnknownGroup = errors.New("no such group")

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

// Entry is one Connection as listed from ssh_config and sshctl metadata.
type Entry struct {
	Alias       string
	Description string
	Group       string
	// Orphaned is true when sshctl metadata exists but the Host alias is gone.
	Orphaned bool
	// Managed is true when the Host block is in sshctl's own ssh_config file,
	// so Host applies and the block can be edited and deleted.
	Managed bool
	Host    Host
}

// Snapshot is the Connection list as it is on disk now.
type Snapshot struct {
	// Entries lists ssh_config's aliases in ssh_config order, then the
	// Orphaned connections sorted by alias.
	Entries []Entry
	// Groups lists every Group in the user's order, including empty ones.
	Groups []Group
	// Identity is the Selected identity's key path, or "" when OpenSSH chooses keys.
	Identity string
	// Collapsed lists the groups the Workspace shows collapsed on this
	// machine, by name (ADR-0004).
	Collapsed []string
	// Include is set when sshctl's hosts file exists but the user's
	// ssh_config does not Include it, so ssh cannot see its Host blocks, as
	// on a new machine the file was synced to (ADR-0004). AddInclude fixes it.
	Include *IncludeError
}

// IncludeError is returned by Save when the user's ssh_config does not yet
// read sshctl's hosts file. Saving can be retried after AddInclude adds Line
// to Config.
type IncludeError struct {
	Line   string
	Config string
}

func (e *IncludeError) Error() string { return openssh.MissingInclude(e.Config, e.Line) }

// Catalog reads and writes Connections in sshctl's config file and
// ssh_config.
type Catalog struct {
	configPath string
	legacy     []string
	ssh        *openssh.Client
	// saveConfig writes the sshctl config and saveHostBlock a Host block in
	// sshctl's ssh_config file; tests make them fail.
	saveConfig    func(string, *config.Config) error
	saveHostBlock func(old string, h openssh.Host) error
}

// New returns the Connection catalog over the sshctl config at configPath
// (migrated from legacy when missing) and ssh.
func New(configPath string, legacy []string, ssh *openssh.Client) *Catalog {
	return &Catalog{configPath: configPath, legacy: legacy, ssh: ssh, saveConfig: config.Save, saveHostBlock: ssh.SaveHost}
}

// Snapshot reads ssh_config's aliases and sshctl metadata fresh from disk,
// so a refresh picks up changes made in another terminal.
func (c *Catalog) Snapshot() (Snapshot, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return Snapshot{}, err
	}
	aliases, err := c.ssh.Aliases()
	if err != nil {
		return Snapshot{}, err
	}
	hosts, err := c.ssh.ManagedHosts()
	if err != nil {
		return Snapshot{}, err
	}
	managed := make(map[string]openssh.Host, len(hosts))
	for _, h := range hosts {
		managed[h.Alias] = h
	}
	snap := Snapshot{Groups: cfg.Groups, Identity: cfg.DefaultKey, Collapsed: cfg.Workspace.CollapsedGroups}
	if snap.Include, err = c.missingInclude(); err != nil {
		return Snapshot{}, err
	}
	seen := map[string]bool{}
	for _, alias := range aliases {
		seen[alias] = true
		m := cfg.Connections[alias]
		e := Entry{Alias: alias, Description: m.Description, Group: m.Group}
		if h, ok := managed[alias]; ok {
			e.Managed = true
			e.Host = h.Endpoint
		}
		snap.Entries = append(snap.Entries, e)
	}
	var orphans []string
	for alias := range cfg.Connections {
		if !seen[alias] {
			orphans = append(orphans, alias)
		}
	}
	sort.Strings(orphans)
	for _, alias := range orphans {
		m := cfg.Connections[alias]
		snap.Entries = append(snap.Entries, Entry{Alias: alias, Description: m.Description, Group: m.Group, Orphaned: true})
	}
	return snap, nil
}

// missingInclude returns the Include the user's ssh_config lacks when
// sshctl's hosts file exists, and nil when there is no such file or it is
// already Included.
func (c *Catalog) missingInclude() (*IncludeError, error) {
	if _, err := os.Stat(c.ssh.ManagedPath()); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	included, err := c.ssh.IncludesManaged()
	if err != nil || included {
		return nil, err
	}
	return c.includeError(), nil
}

func (c *Catalog) includeError() *IncludeError {
	return &IncludeError{Line: openssh.IncludeLine, Config: displayPath(c.ssh.SSHConfig)}
}

// RecordOpen records in History that alias was opened.
func (c *Catalog) RecordOpen(alias string) error {
	return c.update(func(cfg *config.Config) error {
		cfg.RecordOpen(alias)
		return nil
	})
}

// SetGroup puts alias in group ("" for none), keeping its description. The
// Group must already have a record; otherwise SetGroup returns
// ErrUnknownGroup and changes nothing.
func (c *Catalog) SetGroup(alias, group string) error {
	return c.update(func(cfg *config.Config) error {
		if group != "" && cfg.GroupIndex(group) < 0 {
			return fmt.Errorf("%q: %w", group, ErrUnknownGroup)
		}
		cfg.SetMetadata(alias, cfg.Connections[alias].Description, group)
		return nil
	})
}

// SetCollapsed saves groups as the groups the Workspace shows collapsed.
func (c *Catalog) SetCollapsed(groups []string) error {
	return c.update(func(cfg *config.Config) error {
		cfg.Workspace.CollapsedGroups = groups
		return nil
	})
}

// Dotfiles returns the global dotfiles setting: a repo URL,
// config.DotfilesNone, or "" when unset.
func (c *Catalog) Dotfiles() (string, error) {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return "", err
	}
	return cfg.Provision.Dotfiles, nil
}

// SetDotfiles saves the global dotfiles setting ("" unsets it).
func (c *Catalog) SetDotfiles(repo string) error {
	return c.update(func(cfg *config.Config) error {
		cfg.Provision.Dotfiles = repo
		return nil
	})
}

var (
	// ErrNoDotfiles is returned by ProvisionRepo when no dotfiles repo is set.
	ErrNoDotfiles = errors.New("no dotfiles repo is set")
	// ErrProvisionRefused is returned by ProvisionRepo when the setting
	// that applies is config.DotfilesNone.
	ErrProvisionRefused = errors.New("Provision is refused by the dotfiles setting none")
)

// ProvisionRepo returns the dotfiles repo Provision applies to alias's host.
// In v1 that is the global setting.
func (c *Catalog) ProvisionRepo(alias string) (string, error) {
	repo, err := c.Dotfiles()
	switch {
	case err != nil:
		return "", err
	case repo == "":
		return "", ErrNoDotfiles
	case repo == config.DotfilesNone:
		return "", ErrProvisionRefused
	}
	return repo, nil
}

// update loads the sshctl config, applies change and saves it, unless
// change fails.
func (c *Catalog) update(change func(*config.Config) error) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	if err := change(cfg); err != nil {
		return err
	}
	return c.saveConfig(c.configPath, cfg)
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
// first Include sshctl's file, and refuses a Group name config.CheckGroupName
// refuses. A Group with no record gets one, after the others.
func (c *Catalog) Save(old string, s Spec) error {
	if s.Group != "" {
		if err := config.CheckGroupName(s.Group); err != nil {
			return err
		}
	}
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
	h := openssh.Host{Alias: s.Alias, Endpoint: s.Host}
	switch err := c.saveHostBlock(old, h); {
	case errors.Is(err, openssh.ErrNotIncluded):
		return nil, c.includeError()
	case errors.Is(err, openssh.ErrUnknownHost):
		return nil, c.notManaged(old, s.Alias)
	case err != nil:
		return nil, err
	}
	if old == "" {
		return func() error { return c.ssh.DeleteHost(s.Alias) }, nil
	}
	return func() error { return c.ssh.SaveHost(s.Alias, prev) }, nil
}

// notManaged explains why old's Host block, which is not in sshctl's file,
// cannot be saved as alias's.
func (c *Catalog) notManaged(old, alias string) error {
	aliases, err := c.ssh.Aliases()
	switch {
	case err != nil:
		return err
	case !slices.Contains(aliases, old):
		return fmt.Errorf("%s has no Host block in ssh_config to rename", old)
	case old != alias:
		return fmt.Errorf("%s is defined outside sshctl and cannot be renamed here", old)
	}
	return fmt.Errorf("%s's Host block is defined outside sshctl; only its description and group can be edited here", old)
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

// ErrNoMetadata is returned by DeleteMetadata when alias has no sshctl metadata.
var ErrNoMetadata = errors.New("no sshctl metadata")

// DeleteMetadata removes alias's description and group and nothing else:
// its Host block, even one sshctl manages, and its History stay.
func (c *Catalog) DeleteMetadata(alias string) error {
	cfg, err := config.Load(c.configPath, c.legacy)
	if err != nil {
		return err
	}
	if _, ok := cfg.Connections[alias]; !ok {
		return fmt.Errorf("%s: %w", alias, ErrNoMetadata)
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
